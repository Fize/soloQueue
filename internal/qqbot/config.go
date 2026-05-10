package qqbot

// Config holds the QQ Bot configuration.
type Config struct {
	Enabled   bool   `json:"enabled"`
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
	Intents   int    `json:"intents,omitempty"` // 0 = use DefaultIntents()
	Sandbox   bool   `json:"sandbox,omitempty"` // true = use sandbox API
}

// EffectiveIntents returns the intents to use: explicit config value, or DefaultIntents().
func (c Config) EffectiveIntents() int {
	if c.Intents != 0 {
		return c.Intents
	}
	return DefaultIntents()
}

// APIBaseURL returns the QQ OpenAPI base URL based on sandbox mode.
func (c Config) APIBaseURL() string {
	if c.Sandbox {
		return "https://sandbox.api.sgroup.qq.com"
	}
	return "https://api.sgroup.qq.com"
}

// GatewayURL returns the WebSocket gateway URL.
func (c Config) GatewayURL() string {
	if c.Sandbox {
		return "wss://sandbox.api.sgroup.qq.com/websocket/"
	}
	return "wss://api.sgroup.qq.com/websocket/"
}

