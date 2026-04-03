package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	appdb "github.com/ImSingee/git-plus/db"
	"github.com/spf13/cobra"
)

var version = "dev"

const defaultListenAddr = ":8080"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		log.Fatal(err)
	}
}

func newRootCommand() *cobra.Command {
	var listenAddr string
	var dataDir string
	var autoMigrate bool

	cmd := &cobra.Command{
		Use:     "git-plus",
		Short:   "Run the git-plus server",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := newServerConfig(listenAddr, cmd.Flags().Changed("listen"), dataDir, autoMigrate)
			if err != nil {
				return err
			}

			return run(cmd.Context(), cfg)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&listenAddr, "listen", "l", defaultListenAddr, "listen address")
	cmd.Flags().BoolVar(&autoMigrate, "auto-migrate", true, "run embedded database migrations before startup")
	cmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "directory for runtime data")
	cmd.AddCommand(newDBCommand(&dataDir))

	return cmd
}

type serverConfig struct {
	ListenAddr  string
	DataDir     string
	AutoMigrate bool
}

func newServerConfig(listenAddr string, flagChanged bool, dataDir string, autoMigrate bool) (serverConfig, error) {
	normalizedDataDir, err := normalizeDataDir(dataDir)
	if err != nil {
		return serverConfig{}, err
	}

	return serverConfig{
		ListenAddr:  resolveListenAddr(listenAddr, flagChanged),
		DataDir:     normalizedDataDir,
		AutoMigrate: autoMigrate,
	}, nil
}

func newDBCommand(dataDir *string) *cobra.Command {
	dbCommand := &cobra.Command{
		Use:   "db",
		Short: "Database utilities",
	}

	dbCommand.AddCommand(&cobra.Command{
		Use:          "migrate",
		Short:        "Run embedded database migrations",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizedDataDir, err := normalizeDataDir(*dataDir)
			if err != nil {
				return err
			}

			return appdb.Migrate(cmd.Context(), normalizedDataDir)
		},
	})

	return dbCommand
}

func run(ctx context.Context, cfg serverConfig) error {
	if cfg.AutoMigrate {
		if err := appdb.Migrate(ctx, cfg.DataDir); err != nil {
			return fmt.Errorf("run database migrations: %w", err)
		}
	}

	frontendHandler, err := newFrontendHandler()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", apiTestHandler)
	mux.HandleFunc("/api", notFoundAPIHandler)
	mux.HandleFunc("/api/", notFoundAPIHandler)
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/ready", healthzHandler)
	mux.Handle("/", frontendHandler)

	log.Printf("listening on http://localhost%s", cfg.ListenAddr)

	return http.ListenAndServe(cfg.ListenAddr, mux)
}

func resolveListenAddr(flagValue string, flagChanged bool) string {
	if flagChanged {
		return normalizeListenAddr(flagValue)
	}

	if value := os.Getenv("PORT"); value != "" {
		return normalizeListenAddr(value)
	}

	return defaultListenAddr
}

func normalizeListenAddr(value string) string {
	if !strings.Contains(value, ":") {
		return ":" + value
	}

	return value
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func normalizeDataDir(value string) (string, error) {
	normalizedValue := strings.TrimSpace(value)
	if normalizedValue == "" {
		return "", errors.New("--data-dir is required")
	}

	return normalizedValue, nil
}

func notFoundAPIHandler(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func apiTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{
		"ok": true,
	})
}
