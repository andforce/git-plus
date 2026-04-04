package app

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"connectrpc.com/connect"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
	cronv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/cron/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/cron/v1/cronv1connect"
	"github.com/ImSingee/git-plus/pkg/server"
)

func TestNewServerConfigRequiresDataDir(t *testing.T) {
	_, err := newServerConfig(defaultListenAddr, false, "", true)
	if err == nil {
		t.Fatal("expected missing data dir to fail")
	}
	if !strings.Contains(err.Error(), "--data-dir is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewServerConfigAcceptsExplicitDataDir(t *testing.T) {
	cfg, err := newServerConfig("18080", true, " ./tmpdata ", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DataDir != "./tmpdata" {
		t.Fatalf("unexpected data dir: %q", cfg.DataDir)
	}

	if cfg.ListenAddr != ":18080" {
		t.Fatalf("unexpected listen addr: %q", cfg.ListenAddr)
	}

	if cfg.AutoMigrate {
		t.Fatal("expected auto migrate to remain false")
	}
}

func TestDBMigrateDoesNotAcceptAutoMigrateFlag(t *testing.T) {
	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"db", "migrate", "--auto-migrate=false"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag: --auto-migrate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEncryptTokenCommandWritesEncryptedToken(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "correct horse battery staple")

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("secret-token\n"))
	cmd.SetArgs([]string{"config", "encrypt-token"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute encrypt-token: %v", err)
	}

	output := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(output, "$encrypted$1$") {
		t.Fatalf("expected encrypted token output, got %q", output)
	}
}

func TestEncryptTokenCommandRequiresPassphrase(t *testing.T) {
	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("secret-token\n"))
	cmd.SetArgs([]string{"config", "encrypt-token"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing passphrase to fail")
	}
	if !strings.Contains(err.Error(), appconfig.TokenPassphraseEnvVar) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEncryptTokenCommandRequiresTokenOnStdin(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "correct horse battery staple")

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader("\n"))
	cmd.SetArgs([]string{"config", "encrypt-token"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected empty stdin to fail")
	}
	if !strings.Contains(err.Error(), "token is required on stdin") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCronReloadCommandUsesConfiguredServer(t *testing.T) {
	t.Setenv(server.PasswordEnvVar, "test-password")

	mux := http.NewServeMux()
	cronService := &stubCronService{}
	path, handler := cronv1connect.NewCronServiceHandler(cronService)
	mux.Handle(path, handler)
	testServer := httptest.NewServer(http.StripPrefix("/api", mux))
	t.Cleanup(testServer.Close)

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"cron", "reload", "--server", testServer.URL})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cron reload: %v", err)
	}

	if !strings.Contains(stdout.String(), "cron reloaded: enabled") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
	if cronService.gotAuthorization != "Bearer test-password" {
		t.Fatalf("unexpected authorization header: %q", cronService.gotAuthorization)
	}
}

func TestCronReloadCommandUsesPortEnvironmentByDefault(t *testing.T) {
	t.Setenv(server.PasswordEnvVar, "test-password")

	mux := http.NewServeMux()
	cronService := &stubCronService{}
	path, handler := cronv1connect.NewCronServiceHandler(cronService)
	mux.Handle(path, handler)
	testServer := httptest.NewServer(http.StripPrefix("/api", mux))
	t.Cleanup(testServer.Close)

	parsedURL, err := url.Parse(testServer.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	_, port, err := net.SplitHostPort(parsedURL.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	t.Setenv("PORT", port)

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"cron", "reload"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cron reload with PORT: %v", err)
	}

	if !strings.Contains(stdout.String(), "cron reloaded: enabled") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
	if cronService.gotAuthorization != "Bearer test-password" {
		t.Fatalf("unexpected authorization header: %q", cronService.gotAuthorization)
	}
}

func TestCronReloadCommandUsesExplicitListenWhenServerIsNotProvided(t *testing.T) {
	t.Setenv(server.PasswordEnvVar, "test-password")

	mux := http.NewServeMux()
	cronService := &stubCronService{}
	path, handler := cronv1connect.NewCronServiceHandler(cronService)
	mux.Handle(path, handler)
	testServer := httptest.NewServer(http.StripPrefix("/api", mux))
	t.Cleanup(testServer.Close)

	parsedURL, err := url.Parse(testServer.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	_, port, err := net.SplitHostPort(parsedURL.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"cron", "reload", "--listen", port})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute cron reload with listen: %v", err)
	}

	if !strings.Contains(stdout.String(), "cron reloaded: enabled") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
	if cronService.gotAuthorization != "Bearer test-password" {
		t.Fatalf("unexpected authorization header: %q", cronService.gotAuthorization)
	}
}

func TestCronReloadCommandRequiresPasswordEnv(t *testing.T) {
	mux := http.NewServeMux()
	path, handler := cronv1connect.NewCronServiceHandler(&stubCronService{})
	mux.Handle(path, handler)
	testServer := httptest.NewServer(http.StripPrefix("/api", mux))
	t.Cleanup(testServer.Close)

	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"cron", "reload", "--server", testServer.URL})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected missing password env to fail")
	}
	if !strings.Contains(err.Error(), server.PasswordEnvVar) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveAPIPasswordFromEnvRequiresPassword(t *testing.T) {
	_, err := resolveAPIPasswordFromEnv()
	if err == nil {
		t.Fatal("expected missing password env to fail")
	}
	if !strings.Contains(err.Error(), server.PasswordEnvVar) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveAPIPasswordFromEnvReturnsConfiguredPassword(t *testing.T) {
	t.Setenv(server.PasswordEnvVar, " top-secret ")

	password, err := resolveAPIPasswordFromEnv()
	if err != nil {
		t.Fatalf("resolve api password: %v", err)
	}
	if password != "top-secret" {
		t.Fatalf("unexpected password: %q", password)
	}
}

func TestResolveServerURLUsesEmptyServerToDeriveLocalAddress(t *testing.T) {
	if got := resolveServerURL("", defaultListenAddr, false); got != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected default server URL: %q", got)
	}
}

func TestResolveServerURLUsesExplicitServerWhenProvided(t *testing.T) {
	if got := resolveServerURL("http://example.test:9000", defaultListenAddr, false); got != "http://example.test:9000" {
		t.Fatalf("unexpected explicit server URL: %q", got)
	}
}

func TestResolveServerURLUsesListenFlagWhenChanged(t *testing.T) {
	if got := resolveServerURL("", "19090", true); got != "http://127.0.0.1:19090" {
		t.Fatalf("unexpected listen-derived server URL: %q", got)
	}
}

func TestCronCommandFlagsExposeServerAndListen(t *testing.T) {
	cmd := NewRootCommand("test", func() (http.Handler, error) {
		return http.NotFoundHandler(), nil
	})

	cronCmd, _, err := cmd.Find([]string{"cron", "reload"})
	if err != nil {
		t.Fatalf("find cron reload command: %v", err)
	}

	serverFlag := cronCmd.LocalFlags().Lookup("server")
	if serverFlag == nil {
		t.Fatal("expected --server flag")
	}
	if serverFlag.DefValue != "" {
		t.Fatalf("unexpected default server URL: %q", serverFlag.DefValue)
	}

	listenFlag := cronCmd.LocalFlags().Lookup("listen")
	if listenFlag == nil {
		t.Fatal("expected --listen flag")
	}
	if listenFlag.DefValue != defaultListenAddr {
		t.Fatalf("unexpected default listen: %q", listenFlag.DefValue)
	}
}

func TestValidateStartupEnvironmentRequiresEncryptionEnv(t *testing.T) {
	var stderr bytes.Buffer
	err := validateStartupEnvironment(&stderr)
	if err == nil {
		t.Fatal("expected missing encryption env to fail")
	}
	if !strings.Contains(err.Error(), appconfig.TokenPassphraseEnvVar) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStartupEnvironmentRequiresPasswordEnv(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "correct horse battery staple")
	var stderr bytes.Buffer

	err := validateStartupEnvironment(&stderr)
	if err == nil {
		t.Fatal("expected missing password env to fail")
	}
	if !strings.Contains(err.Error(), server.PasswordEnvVar) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStartupEnvironmentAcceptsConfiguredEnvs(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "correct horse battery staple")
	t.Setenv(server.PasswordEnvVar, "top-secret")
	var stderr bytes.Buffer

	if err := validateStartupEnvironment(&stderr); err != nil {
		t.Fatalf("unexpected startup env error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no warning, got %q", stderr.String())
	}
}

func TestValidateStartupEnvironmentWarnsWhenAuthenticationIsDisabled(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "correct horse battery staple")
	t.Setenv(server.PasswordEnvVar, server.InsecureNoAuthPassword)
	var stderr bytes.Buffer

	if err := validateStartupEnvironment(&stderr); err != nil {
		t.Fatalf("unexpected startup env error: %v", err)
	}
	if !strings.Contains(stderr.String(), "disables API authentication") {
		t.Fatalf("expected insecure auth warning, got %q", stderr.String())
	}
}

type stubCronService struct {
	gotAuthorization string
}

func (stubCronService) GetCronRuntime(
	_ context.Context,
	_ *connect.Request[cronv1.GetCronRuntimeRequest],
) (*connect.Response[cronv1.GetCronRuntimeResponse], error) {
	return connect.NewResponse(&cronv1.GetCronRuntimeResponse{}), nil
}

func (stubCronService) UpdateCron(
	_ context.Context,
	_ *connect.Request[cronv1.UpdateCronRequest],
) (*connect.Response[cronv1.UpdateCronResponse], error) {
	return connect.NewResponse(&cronv1.UpdateCronResponse{}), nil
}

func (service *stubCronService) ReloadCron(
	_ context.Context,
	req *connect.Request[cronv1.ReloadCronRequest],
) (*connect.Response[cronv1.ReloadCronResponse], error) {
	service.gotAuthorization = req.Header().Get("Authorization")
	return connect.NewResponse(&cronv1.ReloadCronResponse{
		Runtime: &cronv1.CronRuntime{
			Enabled: testBoolPtr(true),
			Cron:    testStringPtr("0 * * * *"),
		},
	}), nil
}

func testBoolPtr(value bool) *bool {
	return &value
}

func testStringPtr(value string) *string {
	return &value
}
