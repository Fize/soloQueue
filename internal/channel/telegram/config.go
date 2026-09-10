package telegram

import "fmt"

// Config contains the small set of Telegram settings users need to provide.
// Binding to a SoloQueue session is owned by config.TelegramBotConfig.
type Config struct {
	Enabled   bool
	Token     string
	BotID     int64
	AccountID string
	APIURL    string
}

const DefaultAPIURL = "https://api.telegram.org"

func (c Config) EffectiveAPIURL() string {
	if c.APIURL == "" {
		return DefaultAPIURL
	}
	return c.APIURL
}

func (c Config) Validate() error {
	if c.Token == "" {
		return fmt.Errorf("telegram bot token is required")
	}
	return nil
}
