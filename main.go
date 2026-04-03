package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	appconfig "github.com/ImSingee/git-plus/config"
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

	logConfigIssuesOnStartup(cfg.DataDir, log.Default())

	frontendHandler, err := newFrontendHandler()
	if err != nil {
		return err
	}

	log.Printf("listening on http://localhost%s", cfg.ListenAddr)

	return http.ListenAndServe(cfg.ListenAddr, newServerHandler(cfg.DataDir, frontendHandler))
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

func newServerHandler(dataDir string, frontendHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", apiTestHandler)
	mux.HandleFunc("/api/config/check", configCheckHandler(dataDir))
	mux.HandleFunc("/api/config/sources/", sourceConfigCheckHandler(dataDir))
	mux.HandleFunc("/api", notFoundAPIHandler)
	mux.HandleFunc("/api/", notFoundAPIHandler)
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/ready", healthzHandler)
	mux.Handle("/", frontendHandler)

	return mux
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

type checkSummary struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

type configCheckResponse struct {
	Path     string                      `json:"path"`
	Exists   bool                        `json:"exists"`
	Target   string                      `json:"target"`
	SourceID string                      `json:"source_id,omitempty"`
	Issues   []appconfig.ValidationIssue `json:"issues"`
	Summary  checkSummary                `json:"summary"`
}

func configCheckHandler(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		result := appconfig.CheckFile(appconfig.PathForDataDir(dataDir))
		appconfig.SortIssues(result.Issues)
		writeJSON(w, configCheckResponse{
			Path:    result.Path,
			Exists:  result.Exists,
			Target:  "config",
			Issues:  result.Issues,
			Summary: summarizeIssues(result.Issues),
		})
	}
}

func sourceConfigCheckHandler(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sourceID, ok := extractSourceID(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		result := appconfig.CheckSource(appconfig.PathForDataDir(dataDir), sourceID)
		appconfig.SortIssues(result.Issues)
		writeJSON(w, configCheckResponse{
			Path:     result.Path,
			Exists:   result.Exists,
			Target:   "source",
			SourceID: sourceID,
			Issues:   result.Issues,
			Summary:  summarizeIssues(result.Issues),
		})
	}
}

func extractSourceID(requestPath string) (string, bool) {
	const prefix = "/api/config/sources/"
	if !strings.HasPrefix(requestPath, prefix) {
		return "", false
	}

	trimmedPath := strings.TrimPrefix(requestPath, prefix)
	cleanPath := path.Clean("/" + trimmedPath)
	if !strings.HasSuffix(cleanPath, "/check") {
		return "", false
	}

	sourceID := strings.TrimSuffix(strings.TrimPrefix(cleanPath, "/"), "/check")
	if sourceID == "" || strings.Contains(sourceID, "/") {
		return "", false
	}

	return sourceID, true
}

func summarizeIssues(issues []appconfig.ValidationIssue) checkSummary {
	summary := appconfig.Summary(issues)

	return checkSummary{
		Error:   summary[appconfig.SeverityError],
		Warning: summary[appconfig.SeverityWarning],
		Info:    summary[appconfig.SeverityInfo],
	}
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

func logConfigIssuesOnStartup(dataDir string, logger *log.Logger) {
	result := appconfig.CheckFile(appconfig.PathForDataDir(dataDir))
	if !result.Exists {
		return
	}

	appconfig.SortIssues(result.Issues)
	for _, issue := range result.Issues {
		message := fmt.Sprintf("config check %s [%s] %s", issue.Severity, issue.Code, issue.Message)
		if issue.Path != "" {
			message += fmt.Sprintf(" (path: %s)", issue.Path)
		}
		if issue.Line > 0 {
			message += fmt.Sprintf(" (line: %d)", issue.Line)
		}

		logger.Print(message)
	}
}
