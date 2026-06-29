package models

import (
	"fmt"
	"os"
)

type Config struct {
	BotToken    string
	GroqAPIKey  string
	LogLevel    string
	PollingMode string
}

func LoadConfig() (*Config, error) {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("BOT_TOKEN is required")
	}
	groqKey := os.Getenv("GROQ_API_KEY")
	if groqKey == "" {
		return nil, fmt.Errorf("GROQ_API_KEY is required")
	}
	return &Config{
		BotToken:    token,
		GroqAPIKey:  groqKey,
		LogLevel:    envOr("LOG_LEVEL", "info"),
		PollingMode: envOr("POLLING_MODE", "long_polling"),
	}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
