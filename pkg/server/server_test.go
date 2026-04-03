package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
	configv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/config/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/config/v1/configv1connect"
)

const serverTestPassphrase = "correct horse battery staple"

func TestConfigServiceCheckConfigReturnsExistsFalseWhenConfigMissing(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config: %v", err)
	}

	if response.Msg.GetExists() {
		t.Fatal("expected config file to be reported as missing")
	}
	if len(response.Msg.GetIssues()) != 0 {
		t.Fatalf("expected no issues when config is missing, got %#v", response.Msg.GetIssues())
	}
	assertSummaryCounts(t, response.Msg.GetSummary(), 0, 0, 0)
}

func TestConfigServiceCheckConfigReturnsInvalidYAMLIssue(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: [unterminated
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config: %v", err)
	}

	if !response.Msg.GetExists() {
		t.Fatal("expected config file to exist")
	}
	assertHasIssueCode(t, response.Msg.GetIssues(), "invalid_yaml")
	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 0, 0)
}

func TestConfigServiceCheckConfigSummaryMatchesIssues(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources: []
concurrency: 0
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config: %v", err)
	}

	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 1, 0)
}

func TestConfigServiceCheckSourceScopesIssuesToRequestedSource(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
unknown_top_level: true
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
    unknown_source_field: true
  - id: github
    platform: github
    username: hubot
    token: `+encryptedToken+`
concurrency: 0
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckSourceConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckSourceConfigRequest{SourceId: testStringPtr("github")}),
	)
	if err != nil {
		t.Fatalf("check source config: %v", err)
	}

	assertHasIssue(t, response.Msg.GetIssues(), "unknown_field", "sources[0].unknown_source_field")
	assertHasIssue(t, response.Msg.GetIssues(), "duplicate_source_id", "sources[0].id")
	assertHasIssue(t, response.Msg.GetIssues(), "duplicate_source_id", "sources[1].id")
	assertNoIssue(t, response.Msg.GetIssues(), "unknown_field", "unknown_top_level")
	assertNoIssue(t, response.Msg.GetIssues(), "invalid_concurrency", "concurrency")
	assertSummaryCounts(t, response.Msg.GetSummary(), 2, 1, 0)
}

func TestConfigServiceCheckSourceReturnsNotFoundIssue(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckSourceConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckSourceConfigRequest{SourceId: testStringPtr("missing")}),
	)
	if err != nil {
		t.Fatalf("check source config: %v", err)
	}

	assertHasIssue(t, response.Msg.GetIssues(), "source_not_found", "sources")
	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 0, 0)
}

func TestConfigServiceCheckConfigReportsDecryptionFailure(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, "wrong passphrase")
	encryptedToken := mustEncryptServerTokenWithPassphrase(t, "secret", serverTestPassphrase)
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config: %v", err)
	}

	assertHasIssue(t, response.Msg.GetIssues(), "token_decryption_failed", "sources[0].token")
	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 0, 0)
}

func TestConfigServiceGetConfigReturnsDefaultSnapshotWhenMissing(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).GetConfig(
		context.Background(),
		connect.NewRequest(&configv1.GetConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}

	if response.Msg.GetExists() {
		t.Fatal("expected config file to be reported as missing")
	}

	configSnapshot := response.Msg.GetConfig()
	if configSnapshot == nil {
		t.Fatal("expected config snapshot to be present")
	}
	if configSnapshot.GetConcurrency() != int32(appconfig.DefaultConcurrency) {
		t.Fatalf("unexpected concurrency: %d", configSnapshot.GetConcurrency())
	}
	if len(configSnapshot.GetSources()) != 0 {
		t.Fatalf("expected no sources, got %#v", configSnapshot.GetSources())
	}
}

func TestConfigServiceCreateSourceEncryptsAndPersistsToken(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)
	client := newConfigServiceClient(server.URL)

	response, err := client.CreateSource(
		context.Background(),
		connect.NewRequest(&configv1.CreateSourceRequest{
			Source: &configv1.CreateSourceInput{
				Id:               testStringPtr("github-main"),
				Platform:         testPlatformPtr(configv1.Platform_PLATFORM_GITHUB),
				Username:         testStringPtr("octocat"),
				TokenPlaintext:   testStringPtr("super-secret-token"),
				OnlyIncludeRepos: []string{"alpha/repo"},
				ExcludeRepos:     []string{"beta/repo"},
			},
		}),
	)
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	source := response.Msg.GetConfig().GetSources()[0]
	if source.GetToken() == "" {
		t.Fatal("expected encrypted token to be returned")
	}
	if source.GetToken() == "super-secret-token" {
		t.Fatal("expected returned token to stay encrypted")
	}
	if !appconfig.IsEncryptedToken(source.GetToken()) {
		t.Fatalf("expected encrypted token format, got %q", source.GetToken())
	}

	configPath := filepath.Join(dataDir, appconfig.ConfigFilename)
	loaded, err := appconfig.Load(configPath)
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if len(loaded.Data.Sources) != 1 {
		t.Fatalf("expected one persisted source, got %d", len(loaded.Data.Sources))
	}
	if loaded.Data.Sources[0].Token != source.GetToken() {
		t.Fatalf("expected persisted encrypted token to match API response, got %q", loaded.Data.Sources[0].Token)
	}
	if loaded.Data.Sources[0].Token == "super-secret-token" {
		t.Fatal("expected plaintext token to never be written to disk")
	}

	decryptedToken, err := appconfig.DecryptToken(loaded.Data.Sources[0].Token, serverTestPassphrase)
	if err != nil {
		t.Fatalf("decrypt persisted token: %v", err)
	}
	if decryptedToken != "super-secret-token" {
		t.Fatalf("unexpected decrypted token: %q", decryptedToken)
	}
}

func TestConfigServiceGetConfigNeverReturnsPlaintextToken(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: plain-secret
concurrency: 5
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).GetConfig(
		context.Background(),
		connect.NewRequest(&configv1.GetConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}

	sources := response.Msg.GetConfig().GetSources()
	if len(sources) != 1 {
		t.Fatalf("expected one source, got %d", len(sources))
	}
	if sources[0].GetToken() != "" {
		t.Fatalf("expected plaintext token to be withheld, got %q", sources[0].GetToken())
	}
}

func TestConfigServiceUpdateSourcePreservesEncryptedToken(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
    only_include_repos:
      - alpha/repo
concurrency: 5
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).UpdateSource(
		context.Background(),
		connect.NewRequest(&configv1.UpdateSourceRequest{
			SourceId: testStringPtr("github"),
			Patch: &configv1.UpdateSourcePatch{
				Id:       testStringPtr("github-renamed"),
				Username: testStringPtr("hubot"),
				OnlyIncludeRepos: &configv1.StringListValue{
					Values: []string{"renamed/repo"},
				},
				ExcludeRepos: &configv1.StringListValue{
					Values: []string{"ignored/repo"},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("update source: %v", err)
	}

	sources := response.Msg.GetConfig().GetSources()
	if len(sources) != 1 {
		t.Fatalf("expected one source, got %d", len(sources))
	}
	if sources[0].GetId() != "github-renamed" {
		t.Fatalf("unexpected source id: %q", sources[0].GetId())
	}
	if sources[0].GetToken() != encryptedToken {
		t.Fatalf("expected token to remain unchanged, got %q", sources[0].GetToken())
	}

	loaded, err := appconfig.Load(filepath.Join(dataDir, appconfig.ConfigFilename))
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if loaded.Data.Sources[0].ID != "github-renamed" {
		t.Fatalf("unexpected persisted id: %q", loaded.Data.Sources[0].ID)
	}
	if loaded.Data.Sources[0].Token != encryptedToken {
		t.Fatalf("unexpected persisted token: %q", loaded.Data.Sources[0].Token)
	}
}

func TestConfigServiceUpdateSourceSupportsClearingLists(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
    only_include_repos:
      - alpha/repo
    exclude_repos:
      - beta/repo
concurrency: 5
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).UpdateSource(
		context.Background(),
		connect.NewRequest(&configv1.UpdateSourceRequest{
			SourceId: testStringPtr("github"),
			Patch: &configv1.UpdateSourcePatch{
				OnlyIncludeRepos: &configv1.StringListValue{},
				ExcludeRepos:     &configv1.StringListValue{},
			},
		}),
	)
	if err != nil {
		t.Fatalf("update source: %v", err)
	}

	sources := response.Msg.GetConfig().GetSources()
	if len(sources) != 1 {
		t.Fatalf("expected one source, got %d", len(sources))
	}
	if len(sources[0].GetOnlyIncludeRepos()) != 0 {
		t.Fatalf("expected only_include_repos to be cleared, got %#v", sources[0].GetOnlyIncludeRepos())
	}
	if len(sources[0].GetExcludeRepos()) != 0 {
		t.Fatalf("expected exclude_repos to be cleared, got %#v", sources[0].GetExcludeRepos())
	}
}

func TestConfigServiceUpdateSourceRejectsEmptyPatch(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
concurrency: 5
`)
	server := newTestServer(t, dataDir)

	_, err := newConfigServiceClient(server.URL).UpdateSource(
		context.Background(),
		connect.NewRequest(&configv1.UpdateSourceRequest{
			SourceId: testStringPtr("github"),
			Patch:    &configv1.UpdateSourcePatch{},
		}),
	)
	if err == nil {
		t.Fatal("expected empty patch to fail")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument error, got %s", connectErr.Code())
	}
}

func TestConfigServiceReplaceSourceTokenEncryptsNewPlaintext(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	originalToken := mustEncryptServerToken(t, "old-secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+originalToken+`
concurrency: 5
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).ReplaceSourceToken(
		context.Background(),
		connect.NewRequest(&configv1.ReplaceSourceTokenRequest{
			SourceId:       testStringPtr("github"),
			TokenPlaintext: testStringPtr("new-secret"),
		}),
	)
	if err != nil {
		t.Fatalf("replace source token: %v", err)
	}

	source := response.Msg.GetSource()
	if source.GetToken() == "" || !appconfig.IsEncryptedToken(source.GetToken()) {
		t.Fatalf("expected encrypted token, got %q", source.GetToken())
	}
	if source.GetToken() == originalToken {
		t.Fatal("expected token ciphertext to change after replacement")
	}

	decryptedToken, err := appconfig.DecryptToken(source.GetToken(), serverTestPassphrase)
	if err != nil {
		t.Fatalf("decrypt replaced token: %v", err)
	}
	if decryptedToken != "new-secret" {
		t.Fatalf("unexpected decrypted token: %q", decryptedToken)
	}
}

func TestConfigServiceDeleteSourceRemovesSource(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
concurrency: 5
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).DeleteSource(
		context.Background(),
		connect.NewRequest(&configv1.DeleteSourceRequest{
			SourceId: testStringPtr("github"),
		}),
	)
	if err != nil {
		t.Fatalf("delete source: %v", err)
	}

	if len(response.Msg.GetConfig().GetSources()) != 0 {
		t.Fatalf("expected all sources to be removed, got %#v", response.Msg.GetConfig().GetSources())
	}

	loaded, err := appconfig.Load(filepath.Join(dataDir, appconfig.ConfigFilename))
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if len(loaded.Data.Sources) != 0 {
		t.Fatalf("expected persisted config to have no sources, got %d", len(loaded.Data.Sources))
	}
}

func TestConfigServiceCreateSourceUsesBufValidateInterceptor(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	_, err := newConfigServiceClient(server.URL).CreateSource(
		context.Background(),
		connect.NewRequest(&configv1.CreateSourceRequest{
			Source: &configv1.CreateSourceInput{
				Id:             testStringPtr("github"),
				Username:       testStringPtr("octocat"),
				TokenPlaintext: testStringPtr("secret"),
			},
		}),
	)
	if err == nil {
		t.Fatal("expected create source to fail validation")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument error, got %s", connectErr.Code())
	}
}

func TestServerHandlerRoutesConnectAPIAndKeepsLegacyRoutesGone(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)
	client := server.Client()

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config over /api route: %v", err)
	}
	if response.Msg.GetPath() != appconfig.PathForDataDir(dataDir) {
		t.Fatalf("unexpected config path: %q", response.Msg.GetPath())
	}

	for _, route := range []string{
		"/api/config/check",
		"/api/config/sources/github/check",
		"/api/test",
		"/api",
		"/api/",
	} {
		resp, getErr := client.Get(server.URL + route)
		if getErr != nil {
			t.Fatalf("get %s: %v", route, getErr)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected %s to return 404, got %d", route, resp.StatusCode)
		}
	}
}

func TestServerHandlerKeepsHealthzReadyAndFrontendRoutes(t *testing.T) {
	dataDir := t.TempDir()
	server := httptest.NewServer(NewHandler(dataDir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("frontend-ok"))
	})))
	t.Cleanup(server.Close)

	client := server.Client()

	for _, route := range []string{"/healthz", "/ready"} {
		resp, err := client.Get(server.URL + route)
		if err != nil {
			t.Fatalf("get %s: %v", route, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s body: %v", route, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected %s to return 200, got %d", route, resp.StatusCode)
		}
		if string(body) != "ok\n" {
			t.Fatalf("unexpected %s body: %q", route, string(body))
		}
	}

	resp, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get frontend route: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read frontend body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected frontend route to return 200, got %d", resp.StatusCode)
	}
	if string(body) != "frontend-ok" {
		t.Fatalf("unexpected frontend body: %q", string(body))
	}
}

func newTestServer(t *testing.T, dataDir string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(NewHandler(dataDir, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})))
	t.Cleanup(server.Close)

	return server
}

func newConfigServiceClient(serverURL string) configv1connect.ConfigServiceClient {
	return configv1connect.NewConfigServiceClient(
		http.DefaultClient,
		serverURL+"/api",
	)
}

func writeConfigFile(t *testing.T, dataDir string, content string) {
	t.Helper()

	configPath := filepath.Join(dataDir, appconfig.ConfigFilename)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}

func mustEncryptServerToken(t *testing.T, plaintext string) string {
	t.Helper()

	return mustEncryptServerTokenWithPassphrase(t, plaintext, serverTestPassphrase)
}

func mustEncryptServerTokenWithPassphrase(t *testing.T, plaintext string, passphrase string) string {
	t.Helper()

	encryptedToken, err := appconfig.EncryptToken(plaintext, passphrase)
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}

	return encryptedToken
}

func assertSummaryCounts(t *testing.T, summary *configv1.IssueSummary, errors int32, warnings int32, info int32) {
	t.Helper()

	if summary == nil {
		t.Fatal("expected summary to be present")
	}
	if summary.GetError() != errors || summary.GetWarning() != warnings || summary.GetInfo() != info {
		t.Fatalf(
			"unexpected summary counts: error=%d warning=%d info=%d",
			summary.GetError(),
			summary.GetWarning(),
			summary.GetInfo(),
		)
	}
}

func assertHasIssueCode(t *testing.T, issues []*configv1.ValidationIssue, code string) {
	t.Helper()

	for _, issue := range issues {
		if issue.GetCode() == code {
			return
		}
	}

	t.Fatalf("expected issue code=%q, got %#v", code, issues)
}

func assertHasIssue(t *testing.T, issues []*configv1.ValidationIssue, code string, path string) {
	t.Helper()

	for _, issue := range issues {
		if issue.GetCode() != code {
			continue
		}
		if issue.GetPath() != path {
			continue
		}
		return
	}

	t.Fatalf("expected issue code=%q path=%q, got %#v", code, path, issues)
}

func assertNoIssue(t *testing.T, issues []*configv1.ValidationIssue, code string, path string) {
	t.Helper()

	for _, issue := range issues {
		if issue.GetCode() != code {
			continue
		}
		if issue.GetPath() != path {
			continue
		}
		t.Fatalf("did not expect issue code=%q path=%q, got %#v", code, path, issues)
	}
}

func testStringPtr(value string) *string {
	return &value
}

func testPlatformPtr(value configv1.Platform) *configv1.Platform {
	return &value
}
