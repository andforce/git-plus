package app

import (
	"errors"
	"os"
	"strings"

	appdb "github.com/ImSingee/git-plus/db"
	"github.com/ImSingee/git-plus/pkg/server"
	"github.com/spf13/cobra"
)

const defaultListenAddr = ":8080"

func NewRootCommand(version string, frontendHandlerFactory server.FrontendHandlerFactory) *cobra.Command {
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

			return server.Run(cmd.Context(), cfg, frontendHandlerFactory)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&listenAddr, "listen", "l", defaultListenAddr, "listen address")
	cmd.Flags().BoolVar(&autoMigrate, "auto-migrate", true, "run embedded database migrations before startup")
	cmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "directory for runtime data")
	cmd.AddCommand(newDBCommand(&dataDir))

	return cmd
}

func newServerConfig(listenAddr string, flagChanged bool, dataDir string, autoMigrate bool) (server.Config, error) {
	normalizedDataDir, err := normalizeDataDir(dataDir)
	if err != nil {
		return server.Config{}, err
	}

	return server.Config{
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

func normalizeDataDir(value string) (string, error) {
	normalizedValue := strings.TrimSpace(value)
	if normalizedValue == "" {
		return "", errors.New("--data-dir is required")
	}

	return normalizedValue, nil
}
