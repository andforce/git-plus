package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	appdb "github.com/ImSingee/git-plus/db"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
	"github.com/ImSingee/git-plus/pkg/cronruntime"
	"github.com/ImSingee/git-plus/pkg/eventbus"
	configv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/config/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/config/v1/configv1connect"
	cronv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/cron/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/cron/v1/cronv1connect"
	eventv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/event/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/event/v1/eventv1connect"
	taskv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/task/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/task/v1/taskv1connect"
	"github.com/ImSingee/git-plus/pkg/task"
	"github.com/ImSingee/git-plus/pkg/taskservice"
	"github.com/ImSingee/git-plus/pkg/taskstore"
	"google.golang.org/protobuf/types/known/structpb"
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

	assertHasIssueCode(t, response.Msg.GetIssues(), "config_not_found")
	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 0, 0)
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

	assertHasIssueCode(t, response.Msg.GetIssues(), "invalid_yaml")
	assertSummaryCounts(t, response.Msg.GetSummary(), 1, 0, 0)
}

func TestConfigServiceCheckConfigSummaryMatchesIssues(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources: []
concurrency: 0
max_retry_times: -1
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config: %v", err)
	}

	assertSummaryCounts(t, response.Msg.GetSummary(), 2, 1, 0)
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
	if configSnapshot.GetMaxRetryTimes() != int32(appconfig.DefaultMaxRetryTimes) {
		t.Fatalf("unexpected max_retry_times: %d", configSnapshot.GetMaxRetryTimes())
	}
	if configSnapshot.GetCron() != "" {
		t.Fatalf("unexpected cron: %q", configSnapshot.GetCron())
	}
	if len(configSnapshot.GetSources()) != 0 {
		t.Fatalf("expected no sources, got %#v", configSnapshot.GetSources())
	}
}

func TestConfigServiceGetConfigReturnsCronFromConfig(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github
    platform: github
    username: octocat
    token: `+encryptedToken+`
cron: '0 * * * *'
`)
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).GetConfig(
		context.Background(),
		connect.NewRequest(&configv1.GetConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}

	if response.Msg.GetConfig().GetCron() != "0 * * * *" {
		t.Fatalf("unexpected cron: %q", response.Msg.GetConfig().GetCron())
	}
}

func TestConfigServiceUpdateConfigUpdatesGlobalFields(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).UpdateConfig(
		context.Background(),
		connect.NewRequest(&configv1.UpdateConfigRequest{
			Concurrency:   testInt32Ptr(8),
			MaxRetryTimes: testInt32Ptr(5),
		}),
	)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}

	configSnapshot := response.Msg.GetConfig()
	if configSnapshot.GetConcurrency() != 8 {
		t.Fatalf("unexpected concurrency: %d", configSnapshot.GetConcurrency())
	}
	if configSnapshot.GetMaxRetryTimes() != 5 {
		t.Fatalf("unexpected max_retry_times: %d", configSnapshot.GetMaxRetryTimes())
	}

	loaded, err := appconfig.Load(filepath.Join(dataDir, appconfig.ConfigFilename))
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if loaded.Data.Concurrency != 8 {
		t.Fatalf("unexpected persisted concurrency: %d", loaded.Data.Concurrency)
	}
	if loaded.Data.MaxRetryTimes != 5 {
		t.Fatalf("unexpected persisted max_retry_times: %d", loaded.Data.MaxRetryTimes)
	}
}

func TestConfigServiceUpdateConfigSupportsPartialPatch(t *testing.T) {
	dataDir := t.TempDir()
	if err := appconfig.Save(filepath.Join(dataDir, appconfig.ConfigFilename), appconfig.Config{
		Sources:       []appconfig.SourceConfig{},
		Concurrency:   7,
		MaxRetryTimes: 4,
	}); err != nil {
		t.Fatalf("save initial config: %v", err)
	}
	server := newTestServer(t, dataDir)

	response, err := newConfigServiceClient(server.URL).UpdateConfig(
		context.Background(),
		connect.NewRequest(&configv1.UpdateConfigRequest{
			MaxRetryTimes: testInt32Ptr(1),
		}),
	)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}

	configSnapshot := response.Msg.GetConfig()
	if configSnapshot.GetConcurrency() != 7 {
		t.Fatalf("expected concurrency to remain unchanged, got %d", configSnapshot.GetConcurrency())
	}
	if configSnapshot.GetMaxRetryTimes() != 1 {
		t.Fatalf("unexpected max_retry_times: %d", configSnapshot.GetMaxRetryTimes())
	}
}

func TestConfigServiceUpdateConfigRejectsEmptyPatch(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	_, err := newConfigServiceClient(server.URL).UpdateConfig(
		context.Background(),
		connect.NewRequest(&configv1.UpdateConfigRequest{}),
	)
	if err == nil {
		t.Fatal("expected empty config patch to fail")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument error, got %s", connectErr.Code())
	}
}

func TestConfigServiceUpdateConfigUsesBufValidateInterceptor(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	_, err := newConfigServiceClient(server.URL).UpdateConfig(
		context.Background(),
		connect.NewRequest(&configv1.UpdateConfigRequest{
			Concurrency: testInt32Ptr(0),
		}),
	)
	if err == nil {
		t.Fatal("expected invalid config patch to fail validation")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument error, got %s", connectErr.Code())
	}
}

func TestCronServiceGetCronRuntimeReturnsStartupAppliedCron(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, "cron: '0 * * * *'\n")
	server := newTestServer(t, dataDir)

	response, err := newCronServiceClient(server.URL).GetCronRuntime(
		context.Background(),
		connect.NewRequest(&cronv1.GetCronRuntimeRequest{}),
	)
	if err != nil {
		t.Fatalf("get cron runtime: %v", err)
	}

	runtime := response.Msg.GetRuntime()
	if runtime == nil {
		t.Fatal("expected cron runtime")
	}
	if !runtime.GetEnabled() {
		t.Fatal("expected cron to be enabled")
	}
	if runtime.GetCron() != "0 * * * *" {
		t.Fatalf("unexpected cron: %q", runtime.GetCron())
	}
	if len(runtime.GetNextRuns()) != cronruntime.DefaultNextRunsLimit {
		t.Fatalf("expected %d next runs, got %d", cronruntime.DefaultNextRunsLimit, len(runtime.GetNextRuns()))
	}
}

func TestCronServiceGetCronRuntimeReflectsInMemoryState(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)
	client := newCronServiceClient(server.URL)

	if _, err := client.UpdateCron(
		context.Background(),
		connect.NewRequest(&cronv1.UpdateCronRequest{Cron: testStringPtr("0 * * * *")}),
	); err != nil {
		t.Fatalf("update cron: %v", err)
	}

	writeConfigFile(t, dataDir, "cron: '15 * * * *'\n")

	response, err := client.GetCronRuntime(
		context.Background(),
		connect.NewRequest(&cronv1.GetCronRuntimeRequest{}),
	)
	if err != nil {
		t.Fatalf("get cron runtime: %v", err)
	}
	if response.Msg.GetRuntime().GetCron() != "0 * * * *" {
		t.Fatalf("expected in-memory cron to remain unchanged, got %q", response.Msg.GetRuntime().GetCron())
	}
}

func TestCronServiceUpdateCronPersistsAndCanDisable(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)
	client := newCronServiceClient(server.URL)

	enableResponse, err := client.UpdateCron(
		context.Background(),
		connect.NewRequest(&cronv1.UpdateCronRequest{Cron: testStringPtr("0 * * * *")}),
	)
	if err != nil {
		t.Fatalf("enable cron: %v", err)
	}
	if !enableResponse.Msg.GetRuntime().GetEnabled() {
		t.Fatal("expected cron to be enabled")
	}

	loaded, err := appconfig.Load(filepath.Join(dataDir, appconfig.ConfigFilename))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Data.Cron != "0 * * * *" {
		t.Fatalf("unexpected persisted cron: %q", loaded.Data.Cron)
	}

	disableResponse, err := client.UpdateCron(
		context.Background(),
		connect.NewRequest(&cronv1.UpdateCronRequest{Cron: testStringPtr("")}),
	)
	if err != nil {
		t.Fatalf("disable cron: %v", err)
	}
	if disableResponse.Msg.GetRuntime().GetEnabled() {
		t.Fatal("expected cron to be disabled")
	}

	loaded, err = appconfig.Load(filepath.Join(dataDir, appconfig.ConfigFilename))
	if err != nil {
		t.Fatalf("load config after disable: %v", err)
	}
	if loaded.Data.Cron != "" {
		t.Fatalf("expected persisted cron to be empty, got %q", loaded.Data.Cron)
	}
}

func TestCronServiceUpdateCronRejectsInvalidExpression(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	_, err := newCronServiceClient(server.URL).UpdateCron(
		context.Background(),
		connect.NewRequest(&cronv1.UpdateCronRequest{Cron: testStringPtr("0 * 0 0 0")}),
	)
	if err == nil {
		t.Fatal("expected invalid cron to fail")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument, got %s", connectErr.Code())
	}
}

func TestCronServiceReloadCronAppliesEditedConfig(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)
	client := newCronServiceClient(server.URL)

	writeConfigFile(t, dataDir, "cron: '15 * * * *'\n")

	response, err := client.ReloadCron(
		context.Background(),
		connect.NewRequest(&cronv1.ReloadCronRequest{}),
	)
	if err != nil {
		t.Fatalf("reload cron: %v", err)
	}
	if !response.Msg.GetRuntime().GetEnabled() {
		t.Fatal("expected cron to be enabled after reload")
	}
	if response.Msg.GetRuntime().GetCron() != "15 * * * *" {
		t.Fatalf("unexpected cron after reload: %q", response.Msg.GetRuntime().GetCron())
	}
}

func TestCronServiceReloadCronTreatsMissingConfigAsDisabled(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	response, err := newCronServiceClient(server.URL).ReloadCron(
		context.Background(),
		connect.NewRequest(&cronv1.ReloadCronRequest{}),
	)
	if err != nil {
		t.Fatalf("reload cron: %v", err)
	}
	if response.Msg.GetRuntime().GetEnabled() {
		t.Fatal("expected missing config to disable cron")
	}
	if response.Msg.GetRuntime().GetCron() != "" {
		t.Fatalf("unexpected cron for missing config: %q", response.Msg.GetRuntime().GetCron())
	}
}

func TestCronServiceReloadCronDisablesRuntimeWhenConfigIsInvalid(t *testing.T) {
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, "cron: '0 * * * *'\n")
	server := newTestServer(t, dataDir)
	client := newCronServiceClient(server.URL)

	writeConfigFile(t, dataDir, "cron: '0 * 0 0 0'\n")

	_, err := client.ReloadCron(
		context.Background(),
		connect.NewRequest(&cronv1.ReloadCronRequest{}),
	)
	if err == nil {
		t.Fatal("expected invalid config reload to fail")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeFailedPrecondition {
		t.Fatalf("expected failed precondition, got %s", connectErr.Code())
	}

	runtimeResponse, runtimeErr := client.GetCronRuntime(
		context.Background(),
		connect.NewRequest(&cronv1.GetCronRuntimeRequest{}),
	)
	if runtimeErr != nil {
		t.Fatalf("get cron runtime: %v", runtimeErr)
	}
	if runtimeResponse.Msg.GetRuntime().GetEnabled() {
		t.Fatal("expected invalid reload to disable cron runtime")
	}
	if runtimeResponse.Msg.GetRuntime().GetLastError() == "" {
		t.Fatal("expected invalid reload to record last_error")
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
				IncludeDefaults:  testBoolPtr(true),
				IncludeStarred:   testBoolPtr(true),
				IncludeWatching:  testBoolPtr(false),
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
	if !source.GetIncludeDefaults() {
		t.Fatal("expected include_defaults=true")
	}
	if !source.GetIncludeStarred() {
		t.Fatal("expected include_starred=true")
	}
	if source.GetIncludeWatching() {
		t.Fatal("expected include_watching=false")
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
	if !loaded.Data.Sources[0].IncludeDefaults {
		t.Fatal("expected include_defaults to be persisted")
	}
	if !loaded.Data.Sources[0].IncludeStarred {
		t.Fatal("expected include_starred to be persisted")
	}
	if loaded.Data.Sources[0].IncludeWatching {
		t.Fatal("expected include_watching=false to be persisted")
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
    include_defaults: false
    include_starred: true
    include_watching: true
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
	if sources[0].GetIncludeDefaults() {
		t.Fatal("expected include_defaults=false from config")
	}
	if !sources[0].GetIncludeStarred() {
		t.Fatal("expected include_starred=true from config")
	}
	if !sources[0].GetIncludeWatching() {
		t.Fatal("expected include_watching=true from config")
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
				Username: testStringPtr("hubot"),
				OnlyIncludeRepos: &configv1.StringListValue{
					Values: []string{"renamed/repo"},
				},
				ExcludeRepos: &configv1.StringListValue{
					Values: []string{"ignored/repo"},
				},
				IncludeDefaults: testBoolPtr(false),
				IncludeStarred:  testBoolPtr(true),
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
	if sources[0].GetId() != "github" {
		t.Fatalf("unexpected source id: %q", sources[0].GetId())
	}
	if sources[0].GetUsername() != "hubot" {
		t.Fatalf("unexpected username: %q", sources[0].GetUsername())
	}
	if sources[0].GetToken() != encryptedToken {
		t.Fatalf("expected token to remain unchanged, got %q", sources[0].GetToken())
	}
	if sources[0].GetIncludeDefaults() {
		t.Fatal("expected include_defaults=false after patch")
	}
	if !sources[0].GetIncludeStarred() {
		t.Fatal("expected include_starred=true after patch")
	}

	loaded, err := appconfig.Load(filepath.Join(dataDir, appconfig.ConfigFilename))
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if loaded.Data.Sources[0].ID != "github" {
		t.Fatalf("unexpected persisted id: %q", loaded.Data.Sources[0].ID)
	}
	if loaded.Data.Sources[0].Token != encryptedToken {
		t.Fatalf("unexpected persisted token: %q", loaded.Data.Sources[0].Token)
	}
	if loaded.Data.Sources[0].IncludeDefaults {
		t.Fatal("expected persisted include_defaults=false")
	}
	if !loaded.Data.Sources[0].IncludeStarred {
		t.Fatal("expected persisted include_starred=true")
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

func TestTaskServiceEnqueueFullSyncQueuesSourceSyncTasksFromConfig(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: source-a
    platform: github
    username: octocat
    token: `+encryptedToken+`
  - id: source-b
    platform: github
    username: hubot
    token: `+encryptedToken+`
`)
	server := newTestServer(t, dataDir)
	client := newTaskServiceClient(server.URL)

	response, err := client.EnqueueFullSync(
		context.Background(),
		connect.NewRequest(&taskv1.EnqueueFullSyncRequest{}),
	)
	if err != nil {
		t.Fatalf("enqueue full sync: %v", err)
	}
	if response.Msg.GetResult() != taskv1.TaskEnqueueResult_TASK_ENQUEUE_RESULT_STARTED {
		t.Fatalf("unexpected enqueue result: %s", response.Msg.GetResult())
	}
	assertTaskIdentity(t, response.Msg.GetTask(), task.JobTypeSyncAll, task.JobIDSyncAll)
	if response.Msg.GetTask().GetParentTaskId() != "" {
		t.Fatalf("expected sync-all to have no parent task id, got %q", response.Msg.GetTask().GetParentTaskId())
	}
	if response.Msg.GetTask().GetArgs() != nil {
		t.Fatalf("expected sync-all args to be empty, got %#v", response.Msg.GetTask().GetArgs())
	}

	waitUntil(t, func() bool {
		runtimeResponse, runtimeErr := client.GetTaskRuntime(
			context.Background(),
			connect.NewRequest(&taskv1.GetTaskRuntimeRequest{}),
		)
		if runtimeErr != nil {
			t.Fatalf("get task runtime: %v", runtimeErr)
		}

		runningTask := runtimeResponse.Msg.GetRunningTask()
		if runningTask == nil {
			return false
		}
		if runningTask.GetJobId() != task.BuildSourceSyncJobID("source-a") {
			return false
		}
		if runningTask.GetParentTaskId() != response.Msg.GetTask().GetTaskId() {
			return false
		}
		queuedTasks := runtimeResponse.Msg.GetQueuedTasks()
		return len(queuedTasks) == 1 &&
			queuedTasks[0].GetJobId() == task.BuildSourceSyncJobID("source-b") &&
			queuedTasks[0].GetParentTaskId() == response.Msg.GetTask().GetTaskId()
	}, "sync-all to enqueue source sync tasks in config order")
}

func TestTaskServicePersistsStartedTasksToSQLite(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: source-a
    platform: github
    username: octocat
    token: `+encryptedToken+`
  - id: source-b
    platform: github
    username: hubot
    token: `+encryptedToken+`
`)

	server, queries := newPersistentTestServer(t, dataDir)
	client := newTaskServiceClient(server.URL)

	response, err := client.EnqueueFullSync(
		context.Background(),
		connect.NewRequest(&taskv1.EnqueueFullSyncRequest{}),
	)
	if err != nil {
		t.Fatalf("enqueue full sync: %v", err)
	}

	waitUntil(t, func() bool {
		parentRun, parentErr := queries.GetTaskRun(context.Background(), response.Msg.GetTask().GetTaskId())
		if parentErr != nil {
			return false
		}
		if parentRun.Status != string(task.StateFinished) {
			return false
		}

		childA, childAErr := queries.GetTaskRun(context.Background(), "task-2")
		childB, childBErr := queries.GetTaskRun(context.Background(), "task-3")
		if childAErr != nil || childBErr != nil {
			return false
		}
		return childA.ParentTaskID.Valid &&
			childA.ParentTaskID.String == response.Msg.GetTask().GetTaskId() &&
			childB.ParentTaskID.Valid &&
			childB.ParentTaskID.String == response.Msg.GetTask().GetTaskId() &&
			childA.ArgsJson.Valid &&
			childA.ArgsJson.String == `{"source_id":"source-a"}` &&
			childB.ArgsJson.Valid &&
			childB.ArgsJson.String == `{"source_id":"source-b"}` &&
			childA.Status == string(task.StateFinished) &&
			childB.Status == string(task.StateFinished)
	}, "task runs to persist to sqlite")

	logs, err := queries.ListTaskRunLogs(context.Background(), response.Msg.GetTask().GetTaskId())
	if err != nil {
		t.Fatalf("list parent task logs: %v", err)
	}
	if len(logs) < 2 {
		t.Fatalf("expected parent task logs to be persisted, got %d", len(logs))
	}
}

func TestTaskServiceEnqueueSourceSyncValidatesSourceID(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	_, err := newTaskServiceClient(server.URL).EnqueueSourceSync(
		context.Background(),
		connect.NewRequest(&taskv1.EnqueueSourceSyncRequest{SourceId: testStringPtr("missing")}),
	)
	if err == nil {
		t.Fatal("expected missing source to fail")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeNotFound {
		t.Fatalf("expected not found, got %s", connectErr.Code())
	}
}

func TestTaskServiceEnqueueSourceSyncUsesExpectedJobIdentity(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github-main
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)
	server := newTestServer(t, dataDir)

	response, err := newTaskServiceClient(server.URL).EnqueueSourceSync(
		context.Background(),
		connect.NewRequest(&taskv1.EnqueueSourceSyncRequest{SourceId: testStringPtr("github-main")}),
	)
	if err != nil {
		t.Fatalf("enqueue source sync: %v", err)
	}

	assertTaskIdentity(t, response.Msg.GetTask(), task.JobTypeSyncSource, "sync-source::github-main")
	if response.Msg.GetTask().GetParentTaskId() != "" {
		t.Fatalf("expected direct source sync to have no parent task id, got %q", response.Msg.GetTask().GetParentTaskId())
	}
	if response.Msg.GetTask().GetArgs().GetFields()["source_id"].GetStringValue() != "github-main" {
		t.Fatalf("expected source_id in args, got %#v", response.Msg.GetTask().GetArgs())
	}
}

func TestTaskServiceSourceSyncReportsTickingProgress(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github-main
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)
	server := newTestServer(t, dataDir)
	client := newTaskServiceClient(server.URL)

	response, err := client.EnqueueSourceSync(
		context.Background(),
		connect.NewRequest(&taskv1.EnqueueSourceSyncRequest{SourceId: testStringPtr("github-main")}),
	)
	if err != nil {
		t.Fatalf("enqueue source sync: %v", err)
	}

	waitUntil(t, func() bool {
		runtimeResponse, runtimeErr := client.GetTaskRuntime(
			context.Background(),
			connect.NewRequest(&taskv1.GetTaskRuntimeRequest{}),
		)
		if runtimeErr != nil {
			t.Fatalf("get task runtime: %v", runtimeErr)
		}

		runningTask := runtimeResponse.Msg.GetRunningTask()
		if runningTask == nil || runningTask.GetTaskId() != response.Msg.GetTask().GetTaskId() {
			return false
		}

		progress := runningTask.GetProgress()
		if progress == nil {
			return false
		}
		return strings.HasPrefix(progress.GetSummary(), "Processing (")
	}, "source sync progress to tick")
}

func TestTaskServiceCancelQueuedTaskRemovesQueuedEntry(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github-main
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)
	server := newTestServer(t, dataDir)
	client := newTaskServiceClient(server.URL)

	if _, err := client.EnqueueSourceSync(
		context.Background(),
		connect.NewRequest(&taskv1.EnqueueSourceSyncRequest{SourceId: testStringPtr("github-main")}),
	); err != nil {
		t.Fatalf("enqueue first source sync: %v", err)
	}

	waitUntil(t, func() bool {
		runtimeResponse, runtimeErr := client.GetTaskRuntime(
			context.Background(),
			connect.NewRequest(&taskv1.GetTaskRuntimeRequest{}),
		)
		if runtimeErr != nil {
			t.Fatalf("get task runtime: %v", runtimeErr)
		}

		runningTask := runtimeResponse.Msg.GetRunningTask()
		return runningTask != nil && runningTask.GetJobId() == task.BuildSourceSyncJobID("github-main")
	}, "first source sync to start running")

	queuedResponse, err := client.EnqueueSourceSync(
		context.Background(),
		connect.NewRequest(&taskv1.EnqueueSourceSyncRequest{SourceId: testStringPtr("github-main")}),
	)
	if err != nil {
		t.Fatalf("enqueue source sync: %v", err)
	}

	cancelResponse, err := client.CancelQueuedTask(
		context.Background(),
		connect.NewRequest(&taskv1.CancelQueuedTaskRequest{TaskId: testStringPtr(queuedResponse.Msg.GetTask().GetTaskId())}),
	)
	if err != nil {
		t.Fatalf("cancel queued task: %v", err)
	}
	if cancelResponse.Msg.GetTask().GetTaskId() != queuedResponse.Msg.GetTask().GetTaskId() {
		t.Fatalf("unexpected canceled task id: %q", cancelResponse.Msg.GetTask().GetTaskId())
	}

	waitUntil(t, func() bool {
		runtimeResponse, runtimeErr := client.GetTaskRuntime(
			context.Background(),
			connect.NewRequest(&taskv1.GetTaskRuntimeRequest{}),
		)
		if runtimeErr != nil {
			t.Fatalf("get task runtime: %v", runtimeErr)
		}
		return len(runtimeResponse.Msg.GetQueuedTasks()) == 0
	}, "queued task removal")
}

func TestTaskServiceCancelQueuedTaskRejectsRunningTask(t *testing.T) {
	t.Setenv(appconfig.TokenPassphraseEnvVar, serverTestPassphrase)
	encryptedToken := mustEncryptServerToken(t, "secret")
	dataDir := t.TempDir()
	writeConfigFile(t, dataDir, `
sources:
  - id: github-main
    platform: github
    username: octocat
    token: `+encryptedToken+`
`)
	server := newTestServer(t, dataDir)
	client := newTaskServiceClient(server.URL)

	enqueueResponse, err := client.EnqueueSourceSync(
		context.Background(),
		connect.NewRequest(&taskv1.EnqueueSourceSyncRequest{SourceId: testStringPtr("github-main")}),
	)
	if err != nil {
		t.Fatalf("enqueue source sync: %v", err)
	}

	waitUntil(t, func() bool {
		runtimeResponse, runtimeErr := client.GetTaskRuntime(
			context.Background(),
			connect.NewRequest(&taskv1.GetTaskRuntimeRequest{}),
		)
		if runtimeErr != nil {
			t.Fatalf("get task runtime: %v", runtimeErr)
		}

		runningTask := runtimeResponse.Msg.GetRunningTask()
		return runningTask != nil && runningTask.GetTaskId() == enqueueResponse.Msg.GetTask().GetTaskId()
	}, "source sync task to start running")

	_, err = client.CancelQueuedTask(
		context.Background(),
		connect.NewRequest(&taskv1.CancelQueuedTaskRequest{TaskId: testStringPtr(enqueueResponse.Msg.GetTask().GetTaskId())}),
	)
	if err == nil {
		t.Fatal("expected canceling running task to fail")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeFailedPrecondition {
		t.Fatalf("expected failed precondition, got %s", connectErr.Code())
	}
}

func TestEventServiceSubscribeStreamsTaskEvents(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)
	eventClient := newEventServiceClient(server.URL)
	taskClient := newTaskServiceClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamResult := make(chan *connect.ServerStreamForClient[eventv1.SubscribeResponse], 1)
	streamError := make(chan error, 1)
	go func() {
		stream, err := eventClient.Subscribe(
			ctx,
			connect.NewRequest(&eventv1.SubscribeRequest{Channel: testStringPtr("task")}),
		)
		if err != nil {
			streamError <- err
			return
		}
		streamResult <- stream
	}()

	var stream *connect.ServerStreamForClient[eventv1.SubscribeResponse]
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for stream == nil {
		select {
		case err := <-streamError:
			t.Fatalf("subscribe to task events: %v", err)
		case stream = <-streamResult:
		case <-ticker.C:
			if _, err := taskClient.EnqueueTestTask(
				context.Background(),
				connect.NewRequest(&taskv1.EnqueueTestTaskRequest{Variant: testInt32Ptr(2)}),
			); err != nil {
				t.Fatalf("enqueue test task: %v", err)
			}
		case <-deadline:
			t.Fatal("timed out waiting for event stream")
		}
	}

	firstEvent := receiveStreamEvent(t, stream)
	if got := firstEvent.Fields["channel"].GetStringValue(); got != "task" {
		t.Fatalf("unexpected event channel: %q", got)
	}
	if got := firstEvent.Fields["job_type"].GetStringValue(); got != "test" {
		t.Fatalf("unexpected event job_type: %q", got)
	}
	if got := firstEvent.Fields["task_id"].GetStringValue(); got == "" {
		t.Fatal("expected event task_id")
	}

	eventNames := map[string]bool{
		firstEvent.Fields["event_name"].GetStringValue(): true,
	}
	for len(eventNames) < 2 {
		nextEvent := receiveStreamEvent(t, stream)
		eventNames[nextEvent.Fields["event_name"].GetStringValue()] = true
	}
	for eventName := range eventNames {
		switch eventName {
		case "task.enqueued", "task.started", "task.progress", "task.finished":
		default:
			t.Fatalf("unexpected event name set: %v", eventNames)
		}
	}

	cancel()
}

func TestEventServiceRejectsUnknownChannel(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)

	stream, err := newEventServiceClient(server.URL).Subscribe(
		context.Background(),
		connect.NewRequest(&eventv1.SubscribeRequest{Channel: testStringPtr("unknown")}),
	)
	if err != nil {
		t.Fatalf("subscribe should return a stream handle, got %v", err)
	}
	if stream.Receive() {
		t.Fatal("expected invalid channel stream to fail")
	}

	err = stream.Err()
	if err == nil {
		t.Fatal("expected subscribe to fail")
	}
	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected invalid argument, got %s", connectErr.Code())
	}
}

func TestServerHandlerRoutesConnectAPIAndKeepsLegacyRoutesGone(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, dataDir)
	client := server.Client()

	_, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("check config over /api route: %v", err)
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

func TestServerHandlerRejectsUnauthorizedAPIRequests(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServerWithPassword(t, dataDir, "top-secret")

	_, err := newConfigServiceClient(server.URL).CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err == nil {
		t.Fatal("expected unauthenticated request to fail")
	}

	connectErr := new(connect.Error)
	if !errors.As(err, &connectErr) {
		t.Fatalf("expected connect error, got %v", err)
	}
	if connectErr.Code() != connect.CodeUnauthenticated {
		t.Fatalf("expected unauthenticated, got %s", connectErr.Code())
	}
}

func TestServerHandlerAcceptsAuthorizedAPIRequests(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServerWithPassword(t, dataDir, "top-secret")

	response, err := newConfigServiceClientWithPassword(server.URL, "top-secret").CheckConfig(
		context.Background(),
		connect.NewRequest(&configv1.CheckConfigRequest{}),
	)
	if err != nil {
		t.Fatalf("authenticated check config: %v", err)
	}

	assertHasIssueCode(t, response.Msg.GetIssues(), "config_not_found")
}

func TestServerHandlerKeepsHealthzReadyAndFrontendRoutes(t *testing.T) {
	dataDir := t.TempDir()
	taskManager := task.NewManager()
	t.Cleanup(taskManager.Close)
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	cron := mustNewTestCronRuntime(t, dataDir, taskManager)
	server := httptest.NewServer(NewHandler(dataDir, taskManager, bus, cron, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("frontend-ok"))
	}),
		taskservice.WithSourceSyncDuration(func() time.Duration { return 2 * time.Second }),
		taskservice.WithProgressTick(10*time.Millisecond),
	))
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
	return newTestServerWithPassword(t, dataDir, InsecureNoAuthPassword)
}

func newTestServerWithPassword(t *testing.T, dataDir string, password string) *httptest.Server {
	t.Helper()
	t.Setenv(PasswordEnvVar, password)

	taskManager := task.NewManager()
	t.Cleanup(taskManager.Close)
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	cron := mustNewTestCronRuntime(t, dataDir, taskManager)

	server := httptest.NewServer(NewHandler(dataDir, taskManager, bus, cron, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}),
		taskservice.WithSourceSyncDuration(func() time.Duration { return 2 * time.Second }),
		taskservice.WithProgressTick(10*time.Millisecond),
	))
	t.Cleanup(server.Close)

	return server
}

func newPersistentTestServer(t *testing.T, dataDir string) (*httptest.Server, *dbsqlc.Queries) {
	t.Helper()
	t.Setenv(PasswordEnvVar, InsecureNoAuthPassword)

	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate persistent test db: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open persistent test db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	counter := 0
	taskManager := task.NewManager(
		task.WithRecorder(taskstore.NewRecorder(sqliteDB)),
		task.WithIDGenerator(func() string {
			counter++
			return fmt.Sprintf("task-%d", counter)
		}),
	)
	t.Cleanup(taskManager.Close)

	bus := eventbus.New()
	t.Cleanup(bus.Close)
	cron := mustNewTestCronRuntime(t, dataDir, taskManager)

	server := httptest.NewServer(NewHandler(dataDir, taskManager, bus, cron, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}),
		taskservice.WithSourceSyncDuration(func() time.Duration { return 2 * time.Second }),
		taskservice.WithProgressTick(10*time.Millisecond),
	))
	t.Cleanup(server.Close)

	return server, dbsqlc.New(sqliteDB)
}

func newConfigServiceClient(serverURL string) configv1connect.ConfigServiceClient {
	return newConfigServiceClientWithHTTPClient(serverURL, http.DefaultClient)
}

func newConfigServiceClientWithPassword(serverURL string, password string) configv1connect.ConfigServiceClient {
	return newConfigServiceClientWithHTTPClient(serverURL, newAuthenticatedHTTPClient(password))
}

func newConfigServiceClientWithHTTPClient(serverURL string, client *http.Client) configv1connect.ConfigServiceClient {
	return configv1connect.NewConfigServiceClient(
		client,
		serverURL+"/api",
	)
}

func newTaskServiceClient(serverURL string) taskv1connect.TaskServiceClient {
	return taskv1connect.NewTaskServiceClient(
		http.DefaultClient,
		serverURL+"/api",
	)
}

func newCronServiceClient(serverURL string) cronv1connect.CronServiceClient {
	return cronv1connect.NewCronServiceClient(
		http.DefaultClient,
		serverURL+"/api",
	)
}

func newEventServiceClient(serverURL string) eventv1connect.EventServiceClient {
	return eventv1connect.NewEventServiceClient(
		http.DefaultClient,
		serverURL+"/api",
	)
}

func newAuthenticatedHTTPClient(password string) *http.Client {
	return &http.Client{
		Transport: authenticatedTransport{
			base:     http.DefaultTransport,
			password: password,
		},
	}
}

type authenticatedTransport struct {
	base     http.RoundTripper
	password string
}

func (transport authenticatedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clonedRequest := request.Clone(request.Context())
	clonedRequest.Header = request.Header.Clone()
	clonedRequest.Header.Set("Authorization", "Bearer "+transport.password)

	return transport.base.RoundTrip(clonedRequest)
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

func testInt32Ptr(value int32) *int32 {
	return &value
}

func mustNewTestCronRuntime(t *testing.T, dataDir string, taskManager *task.Manager) *cronruntime.Runtime {
	t.Helper()

	cron, err := newCronRuntime(dataDir, taskManager)
	if err != nil {
		t.Fatalf("new cron runtime: %v", err)
	}
	if err := cron.LoadFromFileAndApply(); err != nil {
		t.Logf("load cron runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = cron.Close()
	})

	return cron
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

func assertTaskIdentity(t *testing.T, got *taskv1.Task, jobType string, jobID string) {
	t.Helper()

	if got == nil {
		t.Fatal("expected task")
	}
	if got.GetJobType() != jobType {
		t.Fatalf("unexpected job type: %q", got.GetJobType())
	}
	if got.GetJobId() != jobID {
		t.Fatalf("unexpected job id: %q", got.GetJobId())
	}
}

func waitUntil(t *testing.T, condition func() bool, description string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", description)
}

func receiveStreamEvent(
	t *testing.T,
	stream *connect.ServerStreamForClient[eventv1.SubscribeResponse],
) *structpb.Struct {
	t.Helper()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for stream event")
		default:
		}

		if stream.Receive() {
			return stream.Msg().GetEvent()
		}
		if err := stream.Err(); err != nil {
			t.Fatalf("stream receive failed: %v", err)
		}
	}
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

func testBoolPtr(value bool) *bool {
	return &value
}
