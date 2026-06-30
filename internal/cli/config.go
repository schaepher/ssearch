package cli

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"ssearch/internal/indexer"
	"ssearch/pkg/utils"
)

// Config mirrors the .mysearch.toml structure.
type Config struct {
	Search SearchConfig `toml:"search"`
}

// SearchConfig holds search-related configuration.
type SearchConfig struct {
	Extensions    []string `toml:"extensions"`
	Stopwords     []string `toml:"stopwords"`
	StopwordsFile string   `toml:"stopwords_file"`
	MaxSize       string   `toml:"max_size"`
}

// DefaultConfig returns hard-coded defaults.
func DefaultConfig() *Config {
	return &Config{
		Search: SearchConfig{
			Extensions: nil, // extensions come from utils.SupportedExtensions()
			Stopwords:  nil, // stopwords come from indexer.DefaultStopwords()
			MaxSize:    "50MB",
		},
	}
}

// LoadConfig reads the TOML file at path and merges it with defaults.
// Returns defaults if the file does not exist.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	var fileCfg Config
	if _, err := toml.NewDecoder(f).Decode(&fileCfg); err != nil {
		return nil, err
	}

	// Merge rules per PRD §5.4:
	// - Extensions: append to built-in list
	// - Stopwords: replace (if non-empty)
	// - StopwordsFile: read and replace stopwords
	// - MaxSize: replace if non-empty
	if len(fileCfg.Search.Extensions) > 0 {
		cfg.Search.Extensions = fileCfg.Search.Extensions
	}
	if len(fileCfg.Search.Stopwords) > 0 {
		cfg.Search.Stopwords = fileCfg.Search.Stopwords
	}
	if fileCfg.Search.StopwordsFile != "" {
		data, err := os.ReadFile(fileCfg.Search.StopwordsFile)
		if err != nil {
			return nil, err
		}
		cfg.Search.Stopwords = strings.Fields(string(data))
	}
	if fileCfg.Search.MaxSize != "" {
		cfg.Search.MaxSize = fileCfg.Search.MaxSize
	}

	return cfg, nil
}

// ResolveExtensions returns the full set of supported extensions
// merged with any user-configured extras.
func (c *Config) ResolveExtensions() []string {
	base := utils.SupportedExtensions()
	if len(c.Search.Extensions) > 0 {
		base = append(base, c.Search.Extensions...)
	}
	return base
}

// ResolveStopwords returns the stopword map from config or built-in defaults.
func (c *Config) ResolveStopwords() map[string]bool {
	if len(c.Search.Stopwords) > 0 {
		m := make(map[string]bool, len(c.Search.Stopwords))
		for _, w := range c.Search.Stopwords {
			w = strings.TrimSpace(w)
			if w != "" {
				m[w] = true
			}
		}
		return m
	}
	return indexer.DefaultStopwords()
}
