package server

import (
	"context"
	"fmt"
	"log"
	"net/http"

	appdb "github.com/ImSingee/git-plus/db"
	"github.com/ImSingee/git-plus/pkg/configservice"
)

type Config struct {
	ListenAddr  string
	DataDir     string
	AutoMigrate bool
}

type FrontendHandlerFactory func() (http.Handler, error)

func Run(ctx context.Context, cfg Config, frontendHandlerFactory FrontendHandlerFactory) error {
	if cfg.AutoMigrate {
		if err := appdb.Migrate(ctx, cfg.DataDir); err != nil {
			return fmt.Errorf("run database migrations: %w", err)
		}
	}

	configservice.LogIssuesOnStartup(cfg.DataDir, log.Default())

	frontendHandler, err := frontendHandlerFactory()
	if err != nil {
		return err
	}

	log.Printf("listening on http://localhost%s", cfg.ListenAddr)

	return http.ListenAndServe(cfg.ListenAddr, NewHandler(cfg.DataDir, frontendHandler))
}

func NewHandler(dataDir string, frontendHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", configservice.NewHandler(dataDir))
	mux.HandleFunc("/api", notFoundAPIHandler)
	mux.HandleFunc("/ready", healthzHandler)
	mux.HandleFunc("/healthz", healthzHandler)
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
