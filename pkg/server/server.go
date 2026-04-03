package server

import (
	"context"
	"fmt"
	"log"
	"net/http"

	appdb "github.com/ImSingee/git-plus/db"
	"github.com/ImSingee/git-plus/pkg/configservice"
	"github.com/ImSingee/git-plus/pkg/eventbus"
	"github.com/ImSingee/git-plus/pkg/eventservice"
	"github.com/ImSingee/git-plus/pkg/task"
	"github.com/ImSingee/git-plus/pkg/taskservice"
)

type Config struct {
	ListenAddr  string
	DataDir     string
	AutoMigrate bool
}

type FrontendHandlerFactory func() (http.Handler, error)

func Run(ctx context.Context, cfg Config, frontendHandlerFactory FrontendHandlerFactory) error {
	bus := eventbus.New()
	defer bus.Close()
	taskManager := task.NewManager(
		task.WithLogger(log.Default()),
		task.WithEventBus(bus),
	)
	defer taskManager.Close()

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

	return http.ListenAndServe(cfg.ListenAddr, NewHandler(cfg.DataDir, taskManager, bus, frontendHandler))
}

func NewHandler(dataDir string, taskManager *task.Manager, bus *eventbus.Bus, frontendHandler http.Handler) http.Handler {
	if bus == nil {
		bus = eventbus.New()
	}
	if taskManager == nil {
		taskManager = task.NewManager(
			task.WithLogger(log.Default()),
			task.WithEventBus(bus),
		)
	} else {
		taskManager.SetEventBus(bus)
	}

	mux := http.NewServeMux()
	apiMux := http.NewServeMux()
	configservice.RegisterHandlers(apiMux, dataDir)
	taskservice.RegisterHandlers(apiMux, dataDir, taskManager)
	eventservice.RegisterHandlers(apiMux, bus)
	mux.Handle("/api/", http.StripPrefix("/api", apiMux))
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
