package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	appdb "github.com/ImSingee/git-plus/db"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
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
			if err := validateStartupEnvironment(); err != nil {
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
	cmd.AddCommand(newConfigCommand())

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

func newConfigCommand() *cobra.Command {
	configCommand := &cobra.Command{
		Use:   "config",
		Short: "Configuration utilities",
	}

	configCommand.AddCommand(&cobra.Command{
		Use:          "encrypt-token",
		Short:        "Encrypt a token for use in config.yaml",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			passphrase := os.Getenv(appconfig.TokenPassphraseEnvVar)
			if passphrase == "" {
				return fmt.Errorf("%s is required", appconfig.TokenPassphraseEnvVar)
			}

			input, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read token from stdin: %w", err)
			}

			token := strings.TrimRight(string(input), "\r\n")
			if token == "" {
				return errors.New("token is required on stdin")
			}

			encryptedToken, err := appconfig.EncryptToken(token, passphrase)
			if err != nil {
				return fmt.Errorf("encrypt token: %w", err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), encryptedToken); err != nil {
				return fmt.Errorf("write encrypted token: %w", err)
			}

			return nil
		},
	})

	return configCommand
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

func validateStartupEnvironment() error {
	if os.Getenv(appconfig.TokenPassphraseEnvVar) == "" {
		return fmt.Errorf("%s is required", appconfig.TokenPassphraseEnvVar)
	}

	return nil
}
