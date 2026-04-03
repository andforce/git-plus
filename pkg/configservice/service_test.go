package configservice

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/ImSingee/git-plus/pkg/config"
)

func TestLogIssuesOnStartupLogsTokenDecryptionProblemsWithoutFailing(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "wrong passphrase")

	dataDir := t.TempDir()
	encryptedToken, err := appconfig.EncryptToken("secret", "correct horse battery staple")
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}

	configPath := filepath.Join(dataDir, appconfig.ConfigFilename)
	if err := os.WriteFile(configPath, []byte(`
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	var output bytes.Buffer
	logger := log.New(&output, "", 0)

	LogIssuesOnStartup(dataDir, logger)

	logOutput := output.String()
	if !strings.Contains(logOutput, "token_decryption_failed") {
		t.Fatalf("expected token_decryption_failed in log output, got %q", logOutput)
	}
}
