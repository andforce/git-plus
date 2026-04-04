package cronruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/ImSingee/git-plus/pkg/config"
	"github.com/ImSingee/git-plus/pkg/task"
)

func TestApplyCronEnablesSchedulingAndReturnsNextRuns(t *testing.T) {
	runtime := newTestRuntime(t, t.TempDir(), task.NewManager())

	if err := runtime.ApplyCron("*/5 * * * *"); err != nil {
		t.Fatalf("apply cron: %v", err)
	}

	snapshot, err := runtime.Snapshot(5)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if !snapshot.Enabled {
		t.Fatal("expected cron to be enabled")
	}
	if snapshot.Cron != "*/5 * * * *" {
		t.Fatalf("unexpected cron: %q", snapshot.Cron)
	}
	if len(snapshot.NextRuns) != 5 {
		t.Fatalf("expected 5 next runs, got %d", len(snapshot.NextRuns))
	}
}

func TestApplyCronDisableChangeAndNoChange(t *testing.T) {
	runtime := newTestRuntime(t, t.TempDir(), task.NewManager())

	if err := runtime.ApplyCron("0 * * * *"); err != nil {
		t.Fatalf("apply initial cron: %v", err)
	}
	initialJobID := runtime.job.ID()

	if err := runtime.ApplyCron("0 * * * *"); err != nil {
		t.Fatalf("apply unchanged cron: %v", err)
	}
	if runtime.job == nil {
		t.Fatal("expected job to remain configured")
	}
	if runtime.job.ID() != initialJobID {
		t.Fatal("expected unchanged cron to keep the same job")
	}

	if err := runtime.ApplyCron("15 * * * *"); err != nil {
		t.Fatalf("apply changed cron: %v", err)
	}
	if runtime.job == nil {
		t.Fatal("expected changed cron to keep a job configured")
	}
	if runtime.job.ID() == initialJobID {
		t.Fatal("expected changed cron to replace the job")
	}

	if err := runtime.ApplyCron(""); err != nil {
		t.Fatalf("disable cron: %v", err)
	}
	if runtime.job != nil {
		t.Fatal("expected job to be removed after disabling cron")
	}

	snapshot, err := runtime.Snapshot(10)
	if err != nil {
		t.Fatalf("snapshot after disable: %v", err)
	}
	if snapshot.Enabled {
		t.Fatal("expected disabled snapshot")
	}
	if snapshot.Cron != "" {
		t.Fatalf("expected empty cron after disable, got %q", snapshot.Cron)
	}
}

func TestLoadFromFileAndApplyInvalidConfigDisablesExistingCron(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, appconfig.ConfigFilename)
	runtime := newTestRuntime(t, dataDir, task.NewManager())

	if err := appconfig.Save(configPath, appconfig.Config{
		Sources:       []appconfig.SourceConfig{},
		Concurrency:   appconfig.DefaultConcurrency,
		MaxRetryTimes: appconfig.DefaultMaxRetryTimes,
		Cron:          "0 * * * *",
	}); err != nil {
		t.Fatalf("save valid config: %v", err)
	}
	if err := runtime.LoadFromFileAndApply(); err != nil {
		t.Fatalf("load valid config: %v", err)
	}

	if err := os.WriteFile(configPath, []byte("cron: '0 * 0 0 0'\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	err := runtime.LoadFromFileAndApply()
	if !errors.Is(err, ErrInvalidCronConfig) {
		t.Fatalf("expected invalid cron config error, got %v", err)
	}

	snapshot, snapshotErr := runtime.Snapshot(10)
	if snapshotErr != nil {
		t.Fatalf("snapshot: %v", snapshotErr)
	}
	if snapshot.Enabled {
		t.Fatal("expected invalid reload to disable cron")
	}
	if snapshot.LastError == "" {
		t.Fatal("expected invalid reload to capture last error")
	}
}

func TestCronRunNowEnqueuesSyncAllTask(t *testing.T) {
	manager := task.NewManager()
	runtime, err := New(
		filepath.Join(t.TempDir(), appconfig.ConfigFilename),
		manager,
		WithSyncAllRun(func(ctx *task.ExecutionContext) error {
			if err := ctx.SetProgress("Running cron task", map[string]any{"trigger": "cron"}); err != nil {
				return err
			}
			time.Sleep(50 * time.Millisecond)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = runtime.Close()
		manager.Close()
	})

	if err := runtime.ApplyCron("0 * * * *"); err != nil {
		t.Fatalf("apply cron: %v", err)
	}
	if err := runtime.job.RunNow(); err != nil {
		t.Fatalf("run now: %v", err)
	}

	waitUntilRuntime(t, func() bool {
		runtimeSnapshot := manager.Runtime()
		return runtimeSnapshot.Running != nil && runtimeSnapshot.Running.JobID == task.JobIDSyncAll
	}, "sync-all task to start")
}

func newTestRuntime(t *testing.T, dataDir string, manager *task.Manager) *Runtime {
	t.Helper()

	runtime, err := New(filepath.Join(dataDir, appconfig.ConfigFilename), manager)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = runtime.Close()
		manager.Close()
	})

	return runtime
}

func waitUntilRuntime(t *testing.T, predicate func() bool, description string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", description)
}
