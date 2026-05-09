package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port            int
	DefaultProvider string
	DefaultModel    string
	RequestTimeout  time.Duration

	Providers     []string
	OllamaBaseURL string
	OpenAIBaseURL string
	OpenAIAPIKey  string

	DBPath string
}

func FromEnv() Config {
	return Config{
		Port:            getenvInt("GATEWAY_PORT", 8090),
		DefaultProvider: getenv("GATEWAY_DEFAULT_PROVIDER", "fake"),
		DefaultModel:    getenv("GATEWAY_DEFAULT_MODEL", "fake-1"),
		RequestTimeout:  time.Duration(getenvInt("GATEWAY_REQUEST_TIMEOUT_SECONDS", 60)) * time.Second,

		Providers:     splitCSV(getenv("GATEWAY_PROVIDERS", "fake")),
		OllamaBaseURL: getenv("OLLAMA_BASE_URL", "http://localhost:11434"),
		OpenAIBaseURL: getenv("OPENAI_BASE_URL", "https://api.openai.com"),
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),

		DBPath: getenv("GATEWAY_DB_PATH", "mini-llm-gateway.db"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
