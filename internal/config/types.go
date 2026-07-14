package config

const (
	TGDCFallbackURL  = "https://proxy.lavrush.in/telegram/getProxyConfig"
	TGCFProxyURL     = "https://raw.githubusercontent.com/Flowseal/tg-ws-proxy/main/.github/cfproxy-domains.txt"
	TGFakeSNI        = "storage.googleapis.com"
	TGWSEndpointHost = "149.154.167.220"
)

// FileConfig is the on-disk JSON shape (slim standalone schema).
type FileConfig struct {
	Web   WebConfig   `json:"web"`
	Proxy ProxyConfig `json:"proxy"`
}

type WebConfig struct {
	Port     int    `json:"port"`
	Bind     string `json:"bind"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ProxyConfig mirrors the public settings and is also aliased as MTProtoConfig
// for the extracted mtproto package (via Config.System.MTProto).
type ProxyConfig struct {
	Enabled           bool            `json:"enabled"`
	Port              int             `json:"port"`
	BindAddress       string          `json:"bind_address"`
	MaxConnections    int             `json:"max_connections"`
	TCPUserTimeoutSec int             `json:"tcp_user_timeout_sec"`
	IdleTimeoutSec    int             `json:"idle_timeout_sec"`
	Secrets           []MTProtoSecret `json:"secrets,omitempty"`
	FakeSNI           string          `json:"fake_sni"`
	DCRelay           string          `json:"dc_relay"`
	UpstreamMode      string          `json:"upstream_mode"`
	WSCustomDomain    string          `json:"ws_custom_domain"`
	WSEndpointHost    string          `json:"ws_endpoint_host"`
	CFProxyEnabled    bool            `json:"cfproxy_enabled"`
	CFProxyURL        string          `json:"cfproxy_url"`
	CFWorkerDomain    string          `json:"cfworker_domain"`
	DCFallbackEnabled bool            `json:"dc_fallback_enabled"`
	DCFallbackURL     string          `json:"dc_fallback_url"`

	BridgeSkipNativeEdge bool `json:"-"`
}

// MTProtoConfig is the type name used by the extracted mtproto package.
type MTProtoConfig = ProxyConfig

type MTProtoSecret struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Secret  string `json:"secret"`
	Enabled bool   `json:"enabled"`
}

// Config is the runtime shape expected by the mtproto package.
type Config struct {
	System     SystemConfig `json:"-"`
	Queue      QueueConfig  `json:"-"`
	ConfigPath string       `json:"-"`
	Web        WebConfig    `json:"-"`
}

type SystemConfig struct {
	MTProto MTProtoConfig
}

// QueueConfig kept only for mtproto dial signatures; Mark is always 0
// so we never touch nfqws2 packet marks.
type QueueConfig struct {
	Mark        uint `json:"-"`
	IPv4Enabled bool `json:"-"`
	IPv6Enabled bool `json:"-"`
}
