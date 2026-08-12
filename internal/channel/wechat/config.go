package wechat

const (
	DefaultBaseURL    = "https://ilinkai.weixin.qq.com"
	DefaultCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"
)

type Config struct {
	Enabled    bool
	Token      string
	BotID      string
	BaseURL    string
	CDNBaseURL string
	Version    string
	BotAgent   string
}

func (c Config) EffectiveCDNBaseURL() string {
	if c.CDNBaseURL != "" {
		return c.CDNBaseURL
	}
	return DefaultCDNBaseURL
}

func (c Config) EffectiveBaseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}
