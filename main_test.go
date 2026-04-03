package main

import (
	"io"
	"strings"
	"testing"
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
	cmd := newRootCommand()
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
