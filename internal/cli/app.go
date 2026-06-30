package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"ssearch/internal/storage"
)

// App holds all shared state across subcommands.
type App struct {
	ProjectRoot   string
	DataDir       string
	DBPath        string
	DictCachePath string
	DB            *storage.DB
	Config        *Config
	IncludeHidden bool
	initialized   bool
}

// app is the package-level singleton shared by all subcommands.
var app = &App{}

// Init is idempotent: it runs discovery, config loading, and DB open exactly once.
func (a *App) Init(cmd *cobra.Command) error {
	if a.initialized {
		return nil
	}

	// 1. Discover .ssearch directory.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	projectRoot, dataDir, err := FindOrCreateDataDir(cwd)
	if err != nil {
		return fmt.Errorf("discover data dir: %w", err)
	}
	a.ProjectRoot = projectRoot
	a.DataDir = dataDir

	// 2. Load TOML config.
	cfgPath := filepath.Join(dataDir, ".mysearch.toml")
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	a.Config = cfg

	// 3. Resolve DB path.
	if a.DBPath == "" {
		a.DBPath = filepath.Join(dataDir, "data.db")
	}

	// 4. Dict cache path.
	a.DictCachePath = filepath.Join(dataDir, ".mysearch_dict.gob")

	// 5. Open BoltDB.
	db, err := storage.Open(a.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	a.DB = db

	// 6. Signal handler for graceful shutdown.
	a.setupSignalHandler()

	a.initialized = true
	return nil
}

// Close closes the database connection.
func (a *App) Close() {
	if a.DB != nil {
		_ = a.DB.Close()
		a.DB = nil
	}
}

// setupSignalHandler ensures the DB is closed on Ctrl+C.
func (a *App) setupSignalHandler() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		a.Close()
		os.Exit(1)
	}()
}

// GetDB returns the underlying *bbolt.DB for operations that need it.
func (a *App) GetBoltDB() *storage.DB {
	return a.DB
}

// ResolveRoot returns the effective project root (--root override or discovered).
func (a *App) ResolveRoot(flagRoot string) string {
	if flagRoot != "" {
		return flagRoot
	}
	return a.ProjectRoot
}
