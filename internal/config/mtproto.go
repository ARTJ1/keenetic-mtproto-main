package config

import "strings"

func (m *MTProtoConfig) EffectiveSecrets() []MTProtoSecret {
	out := make([]MTProtoSecret, 0, len(m.Secrets))
	for _, s := range m.Secrets {
		if s.Enabled && strings.TrimSpace(s.Secret) != "" {
			out = append(out, s)
		}
	}
	return out
}

func (m *MTProtoConfig) FirstEnabledSecret() string {
	for _, s := range m.Secrets {
		if s.Enabled && strings.TrimSpace(s.Secret) != "" {
			return s.Secret
		}
	}
	return ""
}
