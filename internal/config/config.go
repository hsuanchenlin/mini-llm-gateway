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

	// Embedder selection (empty disables RAG).
	Embedder         string // "fake" | "ollama" | "openai"
	OllamaEmbedModel string
	OpenAIEmbedModel string

	// Vector store selection (used only when Embedder != "").
	VectorStore      string // "qdrant" | "inmemory"
	QdrantURL        string
	QdrantCollection string

	// RAG behavior knobs.
	RAGTopK      int
	RAGChunkSize int
	RAGOverlap   int
}

func FromEnv() Config {
	return Config{
		Port:            getenvInt("GATEWAY_PORT", 8090),
		DefaultProvider: getenv("GATEWAY_DEFAULT_PROVIDER", "fake"),
		DefaultModel:    getenv("GATEWAY_DEFAULT_MODEL", "fake-1"),
		RequestTimeout:  time.Duration(getenvInt("GATEWAY_REQUEST_TIMEOUT_SECONDS", 120)) * time.Second,

		Providers:     splitCSV(getenv("GATEWAY_PROVIDERS", "fake")),
		OllamaBaseURL: getenv("OLLAMA_BASE_URL", "http://localhost:11434"),
		OpenAIBaseURL: getenv("OPENAI_BASE_URL", "https://api.openai.com"),
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),

		DBPath: getenv("GATEWAY_DB_PATH", "mini-llm-gateway.db"),

		Embedder:         os.Getenv("GATEWAY_EMBEDDER"),
		OllamaEmbedModel: getenv("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
		OpenAIEmbedModel: getenv("OPENAI_EMBED_MODEL", "text-embedding-3-small"),

		VectorStore:      getenv("RAG_VECTOR_STORE", "inmemory"),
		QdrantURL:        getenv("QDRANT_URL", "http://localhost:6333"),
		QdrantCollection: getenv("QDRANT_COLLECTION", "chunks"),

		RAGTopK:      getenvInt("RAG_TOP_K", 4),
		RAGChunkSize: getenvInt("RAG_CHUNK_SIZE", 1000),
		RAGOverlap:   getenvInt("RAG_OVERLAP", 100),
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
