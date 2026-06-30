package models

import (
	"fmt"
	"os"
)

type Config struct {
	BotToken         string
	GroqAPIKey       string
	OpenAIAPIKey     string
	OpenAIModel      string
	AllowedUsersFile string
	PrefsFile        string
	LogLevel         string
	PollingMode      string
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
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	return &Config{
		BotToken:         token,
		GroqAPIKey:       groqKey,
		OpenAIAPIKey:     openAIKey,
		OpenAIModel:      envOr("OPENAI_MODEL", "gpt-4o"),
		AllowedUsersFile: envOr("ALLOWED_USERS_FILE", "allowed_users.txt"),
		PrefsFile:        envOr("PREFS_FILE", "data/prefs.json"),
		LogLevel:         envOr("LOG_LEVEL", "info"),
		PollingMode:      envOr("POLLING_MODE", "long_polling"),
	}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
