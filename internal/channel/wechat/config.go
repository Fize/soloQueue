package wechat

const DefaultBaseURL = "https://ilinkai.weixin.qq.com"

type Config struct {
	Enabled  bool
	Token    string
	BotID    string
	BaseURL  string
	Version  string
	BotAgent string
}

func (c Config) EffectiveBaseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}
