package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/keenetic-mtproto/keenetic-mtproto/internal/config"
	"github.com/keenetic-mtproto/keenetic-mtproto/internal/log"
	"github.com/keenetic-mtproto/keenetic-mtproto/internal/mtproto"
)

type Server struct {
	mu      sync.RWMutex
	cfg     *config.Config
	proxy   *mtproto.Server
	httpSrv *http.Server
	onSave  func(*config.Config) error
}

func New(cfg *config.Config, proxy *mtproto.Server, onSave func(*config.Config) error) *Server {
	return &Server{cfg: cfg, proxy: proxy, onSave: onSave}
}

func (s *Server) getCfg() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) setCfg(cfg *config.Config) {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

func (s *Server) Start() error {
	cfg := s.getCfg()
	mux := http.NewServeMux()
	s.registerAPI(mux)
	mux.Handle("/", spaHandler())

	addr := net.JoinHostPort(cfg.Web.Bind, strconv.Itoa(cfg.Web.Port))
	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           s.auth(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("web listen: %w", err)
	}
	log.Infof("Web UI listening on http://%s", addr)
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Errorf("web server: %v", err)
		}
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.getCfg()
		user := cfg.Web.Username
		pass := cfg.Web.Password
		if user == "" && pass == "" {
			next.ServeHTTP(w, r)
			return
		}
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="keenetic-mtproto"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/config", s.handleFullConfig)
	mux.HandleFunc("/api/mtproto/config", s.handleProxyConfig)
	mux.HandleFunc("/api/mtproto/generate-secret", s.handleGenerateSecret)
	mux.HandleFunc("/api/mtproto/refresh-dcs", s.handleRefreshDCs)
	mux.HandleFunc("/api/mtproto/test-ws", s.handleTestWS)
	mux.HandleFunc("/api/mtproto/sessions", s.handleSessions)
	mux.HandleFunc("/api/mtproto/active-clients", s.handleActiveClients)
	mux.HandleFunc("/api/mtproto/stats", s.handleStats)
}

func (s *Server) handleFullConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"success": true, "config": s.getCfg().ToFile()})
	case http.MethodPost:
		var req config.FileConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if req.Web.Port < 1 || req.Web.Port > 65535 {
			writeErr(w, http.StatusBadRequest, "web.port invalid")
			return
		}
		if err := s.validateProxy(&req.Proxy); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		cur := s.getCfg().Clone()
		cur.ApplyFile(req)
		if err := s.persist(cur); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"success": true, "message": "saved"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleProxyConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"success": true, "config": s.getCfg().System.MTProto})
	case http.MethodPost:
		var req config.MTProtoConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if err := s.validateProxy(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		cur := s.getCfg().Clone()
		cur.System.MTProto = req
		if err := s.persist(cur); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"success": true, "message": "MTProto configuration updated"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) validateProxy(req *config.MTProtoConfig) error {
	if req.Port < 1 || req.Port > 65535 {
		return fmt.Errorf("port must be 1-65535")
	}
	if req.BindAddress != "" && net.ParseIP(req.BindAddress) == nil {
		return fmt.Errorf("invalid bind address")
	}
	if req.MaxConnections < 0 || req.MaxConnections > 100000 {
		return fmt.Errorf("max_connections out of range")
	}
	for i := range req.Secrets {
		sec := &req.Secrets[i]
		sec.Name = sanitizeName(sec.Name)
		sec.Secret = strings.TrimSpace(sec.Secret)
		if sec.Secret != "" {
			if _, err := mtproto.ParseSecret(sec.Secret); err != nil {
				return fmt.Errorf("invalid secret %q: %w", sec.Name, err)
			}
		}
		if sec.ID == "" {
			sec.ID = uuid.NewString()
		}
	}
	if req.Enabled && len(req.EffectiveSecrets()) == 0 && req.FakeSNI == "" {
		return fmt.Errorf("at least one secret or fake_sni required when enabled")
	}
	if req.DCRelay != "" {
		if _, _, err := net.SplitHostPort(req.DCRelay); err != nil {
			return fmt.Errorf("dc_relay must be host:port")
		}
	}
	switch req.UpstreamMode {
	case "", "tcp", "ws", "auto":
	default:
		return fmt.Errorf("upstream_mode must be tcp, ws or auto")
	}
	return nil
}

func (s *Server) persist(cfg *config.Config) error {
	if s.onSave != nil {
		if err := s.onSave(cfg); err != nil {
			return err
		}
	} else if cfg.ConfigPath != "" {
		if err := cfg.SaveToFile(cfg.ConfigPath); err != nil {
			return err
		}
	}
	s.setCfg(cfg)
	if s.proxy != nil {
		s.proxy.UpdateConfig(cfg)
	}
	return nil
}

func (s *Server) handleGenerateSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		FakeSNI string `json:"fake_sni"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.FakeSNI == "" {
		writeErr(w, http.StatusBadRequest, "fake_sni is required")
		return
	}
	sec, err := mtproto.GenerateSecret(req.FakeSNI)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "secret": sec.Hex()})
}

func (s *Server) handleRefreshDCs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mt := s.getCfg().System.MTProto
	if err := mtproto.RefreshDCs(mt.DCFallbackEnabled, mt.DCFallbackURL); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	snap := mtproto.DCSnapshot()
	writeJSON(w, map[string]any{"success": true, "count": len(snap), "dcs": snap})
}

func (s *Server) handleTestWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		UpstreamMode   string  `json:"upstream_mode"`
		WSCustomDomain *string `json:"ws_custom_domain"`
		WSEndpointHost *string `json:"ws_endpoint_host"`
		CFWorkerDomain *string `json:"cfworker_domain"`
		CFProxyEnabled *bool   `json:"cfproxy_enabled"`
		DCRelay        *string `json:"dc_relay"`
		DC             int     `json:"dc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	switch req.UpstreamMode {
	case "", "tcp", "ws", "auto":
	default:
		writeErr(w, http.StatusBadRequest, "upstream_mode must be tcp, ws or auto")
		return
	}
	cfg := s.getCfg()
	probe := cfg.System.MTProto
	if req.UpstreamMode != "" {
		probe.UpstreamMode = req.UpstreamMode
	}
	if req.WSCustomDomain != nil {
		probe.WSCustomDomain = *req.WSCustomDomain
	}
	if req.WSEndpointHost != nil {
		probe.WSEndpointHost = *req.WSEndpointHost
	}
	if req.CFWorkerDomain != nil {
		probe.CFWorkerDomain = *req.CFWorkerDomain
	}
	if req.CFProxyEnabled != nil {
		probe.CFProxyEnabled = *req.CFProxyEnabled
	}
	if req.DCRelay != nil {
		probe.DCRelay = *req.DCRelay
	}
	if probe.UpstreamMode == "" {
		probe.UpstreamMode = "auto"
	}
	dc := req.DC
	if dc == 0 {
		dc = 2
	}
	results, err := mtproto.ProbeTransports(&probe, cfg.Queue, dc)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "dc": dc, "results": results})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessions := s.proxy.Sessions()
	out := make([]map[string]any, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, map[string]any{
			"id":          sess.ID,
			"name":        sess.Name,
			"client_ip":   sess.ClientIP,
			"client_port": sess.ClientPort,
			"destination": sess.Destination,
			"connected_at": sess.ConnectedAt.UTC().Format(time.RFC3339),
			"last_seen":    sess.LastSeen.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleActiveClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	type row struct {
		ID                string   `json:"id"`
		Name              string   `json:"name"`
		ActiveConnections int      `json:"active_connections"`
		ActiveIPs         []string `json:"active_ips"`
		ActiveIPCount     int      `json:"active_ip_count"`
		LastSeen          string   `json:"last_seen"`
	}
	type agg struct {
		row
		seen map[string]struct{}
		last time.Time
	}
	order := make([]string, 0)
	byID := map[string]*agg{}
	for _, sess := range s.proxy.Sessions() {
		a := byID[sess.ID]
		if a == nil {
			a = &agg{row: row{ID: sess.ID, Name: sess.Name, ActiveIPs: []string{}}, seen: map[string]struct{}{}}
			byID[sess.ID] = a
			order = append(order, sess.ID)
		}
		a.ActiveConnections++
		if sess.ClientIP != "" {
			if _, ok := a.seen[sess.ClientIP]; !ok {
				a.seen[sess.ClientIP] = struct{}{}
				a.ActiveIPs = append(a.ActiveIPs, sess.ClientIP)
			}
		}
		if sess.LastSeen.After(a.last) {
			a.last = sess.LastSeen
		}
	}
	out := make([]row, 0, len(order))
	for _, id := range order {
		a := byID[id]
		a.ActiveIPCount = len(a.ActiveIPs)
		a.LastSeen = a.last.UTC().Format(time.RFC3339)
		out = append(out, a.row)
	}
	writeJSON(w, out)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.proxy.Stats())
}

func sanitizeName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, name)
	return strings.TrimSpace(cleaned)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": msg})
}

func spaHandler() http.Handler {
	sub, err := fs.Sub(uiFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not embedded", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err != nil {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
