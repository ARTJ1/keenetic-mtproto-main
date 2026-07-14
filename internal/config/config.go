package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	saveMu sync.Mutex
)

func DefaultFileConfig() FileConfig {
	return FileConfig{
		Web: WebConfig{
			Port:     7788,
			Bind:     "0.0.0.0",
			Username: "admin",
			Password: "admin",
		},
		Proxy: ProxyConfig{
			Enabled:           true,
			Port:              8443,
			BindAddress:       "0.0.0.0",
			FakeSNI:           TGFakeSNI,
			UpstreamMode:      "auto",
			CFProxyEnabled:    true,
			CFProxyURL:        TGCFProxyURL,
			DCFallbackEnabled: true,
			DCFallbackURL:     TGDCFallbackURL,
		},
	}
}

// DefaultConfig satisfies tests that reference config.DefaultConfig.System.MTProto.
var DefaultConfig = Config{
	System: SystemConfig{
		MTProto: DefaultFileConfig().Proxy,
	},
	Queue: QueueConfig{IPv4Enabled: true, Mark: 0},
}

func (c *Config) ApplyFile(f FileConfig) {
	c.Web = f.Web
	c.System.MTProto = f.Proxy
	c.Queue.Mark = 0
	c.Queue.IPv4Enabled = true
	c.Queue.IPv6Enabled = false
}

func (c *Config) ToFile() FileConfig {
	return FileConfig{
		Web:   c.Web,
		Proxy: c.System.MTProto,
	}
}

func (c *Config) Clone() *Config {
	raw, _ := json.Marshal(c.ToFile())
	var f FileConfig
	_ = json.Unmarshal(raw, &f)
	out := &Config{ConfigPath: c.ConfigPath}
	out.ApplyFile(f)
	return out
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			f := DefaultFileConfig()
			cfg := &Config{ConfigPath: path}
			cfg.ApplyFile(f)
			if err := cfg.SaveToFile(path); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, err
	}
	var f FileConfig
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&f)
	cfg := &Config{ConfigPath: path}
	cfg.ApplyFile(f)
	return cfg, nil
}

func applyDefaults(f *FileConfig) {
	d := DefaultFileConfig()
	if f.Web.Port == 0 {
		f.Web.Port = d.Web.Port
	}
	if f.Web.Bind == "" {
		f.Web.Bind = d.Web.Bind
	}
	if f.Web.Username == "" {
		f.Web.Username = d.Web.Username
	}
	if f.Proxy.Port == 0 {
		f.Proxy.Port = d.Proxy.Port
	}
	if f.Proxy.BindAddress == "" {
		f.Proxy.BindAddress = d.Proxy.BindAddress
	}
	if f.Proxy.FakeSNI == "" {
		f.Proxy.FakeSNI = d.Proxy.FakeSNI
	}
	if f.Proxy.UpstreamMode == "" {
		f.Proxy.UpstreamMode = d.Proxy.UpstreamMode
	}
	if f.Proxy.CFProxyURL == "" && f.Proxy.CFProxyEnabled {
		f.Proxy.CFProxyURL = d.Proxy.CFProxyURL
	}
	if f.Proxy.DCFallbackURL == "" && f.Proxy.DCFallbackEnabled {
		f.Proxy.DCFallbackURL = d.Proxy.DCFallbackURL
	}
}

func (c *Config) SaveToFile(path string) error {
	if path == "" {
		return fmt.Errorf("empty config path")
	}
	saveMu.Lock()
	defer saveMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.ToFile(), "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
