package mtproto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/keenetic-mtproto/keenetic-mtproto/internal/config"
	"github.com/keenetic-mtproto/keenetic-mtproto/internal/log"
)

const (
	obfuscatedFrameLen    = 64
	connectionTagAbridged = 0xefefefef
	connectionTagInter    = 0xeeeeeeee
	connectionTagPadded   = 0xdddddddd

	telegramWSEdgeIP = "149.154.167.220"
	wsDialTimeout    = 8 * time.Second
	tcpDialTimeout   = 8 * time.Second
)

var wsEdgeServedDCs = map[int]bool{2: true, 4: true}

func wsEdgeServesDC(absDC int) bool {
	return wsEdgeServedDCs[absDC]
}

func wsNativeDialHost(override string) string {
	if h := strings.TrimSpace(override); h != "" {
		return h
	}
	return telegramWSEdgeIP
}

func normalizeWorkerDomain(d string) string {
	d = strings.TrimSpace(d)
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	return strings.TrimSpace(d)
}

func workerDomains(cfg *config.MTProtoConfig) []string {
	raw := strings.TrimSpace(cfg.CFWorkerDomain)
	if raw == "" {
		return nil
	}
	var out []string
	for _, d := range strings.Split(raw, ",") {
		if d = normalizeWorkerDomain(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

func workerDstIP(absDC int) string {
	addr, ok := dcAddressesV4[absDC]
	if !ok {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

type ObfuscatedConn struct {
	net.Conn
	reader cipher.Stream
	writer cipher.Stream
}

func (c *ObfuscatedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.reader.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (c *ObfuscatedConn) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	c.writer.XORKeyStream(buf, p)
	return c.Conn.Write(buf)
}

type ClientHandshakeResult struct {
	DC       int
	ProtoTag uint32
	Conn     *ObfuscatedConn
}

func AcceptObfuscated(conn net.Conn, secret *Secret) (*ClientHandshakeResult, error) {
	return acceptObfuscatedFrame(conn, func(raw []byte) []byte {
		return deriveKey(raw, secret.Key[:])
	})
}

func acceptObfuscatedFrame(conn net.Conn, derive func(raw []byte) []byte) (*ClientHandshakeResult, error) {
	frame := make([]byte, obfuscatedFrameLen)
	if _, err := io.ReadFull(conn, frame); err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	return decodeObfuscatedFrame(frame, conn, derive)
}

func decodeObfuscatedDirect(frame []byte, conn net.Conn) (*ClientHandshakeResult, error) {
	return decodeObfuscatedFrame(frame, conn, func(raw []byte) []byte {
		out := make([]byte, len(raw))
		copy(out, raw)
		return out
	})
}

func decodeObfuscatedFrame(frame []byte, conn net.Conn, derive func(raw []byte) []byte) (*ClientHandshakeResult, error) {
	decIV := make([]byte, 16)
	copy(decIV, frame[40:56])
	decStream, err := newAESCTR(derive(frame[8:40]), decIV)
	if err != nil {
		return nil, fmt.Errorf("init decrypt: %w", err)
	}

	reversed := make([]byte, 48)
	for i := 0; i < 48; i++ {
		reversed[i] = frame[55-i]
	}
	encIV := make([]byte, 16)
	copy(encIV, reversed[32:48])
	encStream, err := newAESCTR(derive(reversed[:32]), encIV)
	if err != nil {
		return nil, fmt.Errorf("init encrypt: %w", err)
	}

	decrypted := make([]byte, obfuscatedFrameLen)
	copy(decrypted, frame)
	decStream.XORKeyStream(decrypted, decrypted)

	tag := binary.LittleEndian.Uint32(decrypted[56:60])
	switch tag {
	case connectionTagAbridged, connectionTagInter, connectionTagPadded:
	default:
		return nil, fmt.Errorf("invalid connection tag: 0x%08x", tag)
	}

	dc := int(int16(binary.LittleEndian.Uint16(decrypted[60:62])))

	return &ClientHandshakeResult{
		DC:       dc,
		ProtoTag: tag,
		Conn: &ObfuscatedConn{
			Conn:   conn,
			reader: decStream,
			writer: encStream,
		},
	}, nil
}

type transportKind int

const (
	transportTCP transportKind = iota
	transportWS
)

type transportPlan struct {
	kind     transportKind
	addr     string
	sni      string
	dialHost string
	dc       int
	cfBase   string
	wsPath   string
	isWorker bool
}

func (p transportPlan) describe() string {
	switch p.kind {
	case transportWS:
		if p.isWorker {
			return "wsworker://" + p.sni
		}
		if p.dialHost != "" && p.dialHost != p.sni {
			return fmt.Sprintf("ws://%s@%s", p.sni, p.dialHost)
		}
		return "ws://" + p.sni
	default:
		return "tcp://" + p.addr
	}
}

// cooldownKey is the host:port we actually dial. Native kws2 and kws2-1 share
// telegramWSEdgeIP, so one TCP timeout must skip the sibling instead of burning
// another tcpDialTimeout (Telegram clients usually give up before 16s).
func (p transportPlan) cooldownKey() string {
	if p.kind == transportTCP {
		return p.addr
	}
	host := p.dialHost
	if host == "" {
		host = p.sni
	}
	if host == "" {
		return ""
	}
	return hostPort443(host)
}

func hostPort443(host string) string {
	if host == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, "443")
}

func planTransports(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int) ([]transportPlan, error) {
	absDC := dc
	if absDC < 0 {
		absDC = -absDC
	}

	mode := cfg.UpstreamMode
	if mode == "" {
		mode = "tcp"
	}

	hasRelay := strings.TrimSpace(cfg.DCRelay) != ""
	relayFirst := mode == "auto" && hasRelay

	var plans []transportPlan

	appendTCP := func() {
		addrs, err := ResolveDCAll(dc, queueCfg.IPv6Enabled, strings.TrimSpace(cfg.DCRelay))
		if err != nil {
			return
		}
		for _, a := range addrs {
			if tcpAddrInCooldown(a) {
				continue
			}
			plans = append(plans, transportPlan{kind: transportTCP, addr: a})
		}
	}

	if mode == "tcp" || relayFirst {
		appendTCP()
	}

	wsMode := mode == "ws" || mode == "auto"
	wsBlacklisted := wsMode && wsIsBlacklisted(dc)
	if wsBlacklisted {
		log.Debugf("%s DC %d WS plans skipped (blacklisted)", tg(""), dc)
	}
	if wsMode && !wsBlacklisted {
		if wsEdgeServesDC(absDC) && !cfg.BridgeSkipNativeEdge {
			dh := wsNativeDialHost(cfg.WSEndpointHost)
			edgeKey := hostPort443(dh)
			if tcpAddrInCooldown(edgeKey) {
				log.Debugf("%s DC %d native WS edge %s skipped (TCP cooldown)", tg(""), absDC, edgeKey)
			} else {
				primary := transportPlan{kind: transportWS, dc: dc, sni: fmt.Sprintf("kws%d.web.telegram.org", absDC), dialHost: dh}
				media := transportPlan{kind: transportWS, dc: dc, sni: fmt.Sprintf("kws%d-1.web.telegram.org", absDC), dialHost: dh}
				if dc < 0 {
					plans = append(plans, media, primary)
				} else {
					plans = append(plans, primary, media)
				}
			}
		}
		if dst := workerDstIP(absDC); dst != "" {
			for _, wd := range workerDomains(cfg) {
				if workerInCooldown(wd) {
					log.Debugf("%s DC %d worker %s skipped (cooldown)", tg(""), absDC, wd)
					continue
				}
				plans = append(plans, transportPlan{
					kind:     transportWS,
					dc:       dc,
					sni:      wd,
					dialHost: wd,
					wsPath:   fmt.Sprintf("/apiws?dst=%s&dc=%d", dst, absDC),
					isWorker: true,
				})
			}
		}
		if d := strings.TrimSpace(cfg.WSCustomDomain); d != "" {
			plans = append(plans, transportPlan{
				kind:   transportWS,
				dc:     dc,
				sni:    fmt.Sprintf("kws%d.%s", absDC, d),
				cfBase: d,
			})
		}
		if cfg.CFProxyEnabled {
			for _, base := range cfBalancerInst.domainsForDC(dc) {
				sni := fmt.Sprintf("kws%d.%s", absDC, base)
				if dc < 0 {
					sni = fmt.Sprintf("kws%d-1.%s", absDC, base)
				}
				plans = append(plans, transportPlan{
					kind:   transportWS,
					dc:     dc,
					sni:    sni,
					cfBase: base,
				})
			}
		}
	}

	if mode == "auto" && !relayFirst {
		appendTCP()
	}

	if len(plans) == 0 && mode != "ws" {
		appendTCP()
	}

	if len(plans) == 0 {
		return nil, fmt.Errorf("no transports available for DC %d (mode=%s)", absDC, mode)
	}
	return plans, nil
}

func DialObfuscatedDC(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int, protoTag uint32) (*ObfuscatedConn, string, error) {
	return DialObfuscatedDCWithPool(cfg, queueCfg, dc, protoTag, nil, "")
}

func DialObfuscatedDCWithPool(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int, protoTag uint32, pool *wsPool, logID string) (*ObfuscatedConn, string, error) {
	tag := tg(logID)
	if pool != nil && !wsIsBlacklisted(dc) {
		if raw := pool.get(dc); raw != nil {
			obf, err := completeObfuscation(raw, dc, protoTag)
			if err == nil && raw.liveNow() {
				log.Infof("%s DC %d connected via ws-pool", tag, dc)
				wsRecordSuccess(dc)
				return obf, "ws-pool", nil
			}
			if err != nil {
				log.Debugf("%s DC %d pool conn obf init failed: %v", tag, dc, err)
			} else {
				log.Debugf("%s DC %d pool conn died before relay; re-dialing fresh", tag, dc)
			}
			_ = raw.Close()
		}
	}

	plans, err := planTransports(cfg, queueCfg, dc)
	if err != nil {
		return nil, "", err
	}

	wsTimeout := wsDialTimeout
	if wsCooldownActive(dc) {
		wsTimeout = wsDialTimeoutCooldown
	}

	return dialPlans(plans, queueCfg.Mark, dc, protoTag, wsTimeout, tag)
}

func isDialTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return strings.Contains(err.Error(), "i/o timeout")
}

func shortErr(err error) string {
	s := err.Error()
	for _, p := range []string{"tcp dial ", "tls handshake ", "ws read response: ", "ws write upgrade: ", "ws handshake "} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

func isWSRedirect(err error) bool {
	var he *wsHandshakeError
	if !errors.As(err, &he) {
		return false
	}
	return he.isRedirect()
}

func wsRateLimited(err error) bool {
	var he *wsHandshakeError
	if !errors.As(err, &he) {
		return false
	}
	return he.statusCode == 429 || he.statusCode == 503
}

func dialOneWS(p transportPlan, mark uint, timeout time.Duration) (net.Conn, error) {
	host := p.dialHost
	if host == "" {
		host = p.sni
	}
	return dialWS(host, p.sni, p.wsPath, timeout, mark)
}

type TransportProbeResult struct {
	Transport string `json:"transport"`
	OK        bool   `json:"ok"`
	Stage     string `json:"stage,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	HoldMs    int64  `json:"hold_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

const probeHoldDuration = 2 * time.Second

func ProbeTransports(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int) ([]TransportProbeResult, error) {
	plans, err := planTransports(cfg, queueCfg, dc)
	if err != nil {
		return nil, err
	}
	// Probe every planned path in parallel and ignore live-dial cooldowns so
	// the UI shows the real picture (native WS / TCP / Worker / CF pool).
	out := make([]TransportProbeResult, len(plans))
	var wg sync.WaitGroup
	for i, p := range plans {
		wg.Add(1)
		go func(i int, p transportPlan) {
			defer wg.Done()
			out[i] = probeOne(p, queueCfg.Mark, dc)
		}(i, p)
	}
	wg.Wait()
	return out, nil
}

func probeOne(p transportPlan, mark uint, dc int) TransportProbeResult {
	res := TransportProbeResult{Transport: p.describe()}
	dialStart := time.Now()
	conn, err := dialOne(p, mark)
	if err != nil {
		res.Stage = "dial"
		res.Error = err.Error()
		return res
	}
	res.LatencyMs = time.Since(dialStart).Milliseconds()
	defer conn.Close()

	if _, err := completeObfuscation(conn, dc, connectionTagAbridged); err != nil {
		res.Stage = "handshake"
		res.Error = err.Error()
		return res
	}

	_ = conn.SetReadDeadline(time.Now().Add(probeHoldDuration))
	holdStart := time.Now()
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	res.HoldMs = time.Since(holdStart).Milliseconds()

	if readErr == nil {
		res.OK = true
		return res
	}
	if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
		res.OK = true
		return res
	}
	res.Stage = "hold"
	res.Error = "upstream closed connection: " + readErr.Error()
	return res
}

func dialOne(p transportPlan, mark uint) (net.Conn, error) {
	switch p.kind {
	case transportWS:
		host := p.dialHost
		if host == "" {
			host = p.sni
		}
		return dialWS(host, p.sni, p.wsPath, wsDialTimeout, mark)
	default:
		dialer := net.Dialer{Timeout: tcpDialTimeout}
		conn, err := dialer.Dial("tcp", p.addr)
		if err != nil {
			if isDialTimeout(err) {
				tcpRecordFailure(p.addr)
			}
			return nil, err
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
			setTCPUserTimeout(tc, defaultUserTimeout)
		}
		return conn, nil
	}
}

func completeObfuscation(conn net.Conn, dc int, protoTag uint32) (*ObfuscatedConn, error) {
	frame := generateFrame(dc, protoTag)

	encKey := frame[8:40]
	encIV := make([]byte, 16)
	copy(encIV, frame[40:56])
	encStream, err := newAESCTR(encKey, encIV)
	if err != nil {
		return nil, fmt.Errorf("init encrypt: %w", err)
	}

	reversed := make([]byte, 48)
	for i := 0; i < 48; i++ {
		reversed[i] = frame[55-i]
	}
	decKey := reversed[:32]
	decIV := make([]byte, 16)
	copy(decIV, reversed[32:48])
	decStream, err := newAESCTR(decKey, decIV)
	if err != nil {
		return nil, fmt.Errorf("init decrypt: %w", err)
	}

	encrypted := make([]byte, obfuscatedFrameLen)
	copy(encrypted, frame)
	encStream.XORKeyStream(encrypted, encrypted)
	copy(encrypted[0:56], frame[0:56])

	if _, err := conn.Write(encrypted); err != nil {
		return nil, fmt.Errorf("send handshake: %w", err)
	}

	return &ObfuscatedConn{
		Conn:   conn,
		reader: decStream,
		writer: encStream,
	}, nil
}

var reservedFirst4Words = []uint32{
	0x44414548,
	0x54534f50,
	0x20544547,
	0x4954504f,
	0x02010316,
	0xdddddddd,
	0xeeeeeeee,
}

func isReservedFirst4(b []byte) bool {
	if b[0] == 0xef {
		return true
	}
	first4 := binary.LittleEndian.Uint32(b[:4])
	for _, w := range reservedFirst4Words {
		if first4 == w {
			return true
		}
	}
	return false
}

func generateFrame(dc int, protoTag uint32) []byte {
	frame := make([]byte, obfuscatedFrameLen)
	for {
		if _, err := rand.Read(frame); err != nil {
			continue
		}

		if isReservedFirst4(frame[0:4]) {
			continue
		}
		if binary.LittleEndian.Uint32(frame[4:8]) == 0 {
			continue
		}
		break
	}

	binary.LittleEndian.PutUint32(frame[56:60], protoTag)
	binary.LittleEndian.PutUint16(frame[60:62], uint16(int16(dc)))
	return frame
}

func deriveKey(rawKey []byte, secret []byte) []byte {
	h := sha256.New()
	h.Write(rawKey)
	h.Write(secret)
	return h.Sum(nil)
}

func newAESCTR(key, iv []byte) (cipher.Stream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, iv), nil
}
