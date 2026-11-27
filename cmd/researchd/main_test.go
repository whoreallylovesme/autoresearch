package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/operatorauth"
)

func TestDaemonFlagsKeepNetworkBoundaryExplicit(t *testing.T) {
	root := newRootCommand()
	for _, name := range []string{"config", "listen", "data-dir", "allow-non-loopback"} {
		if root.Flag(name) == nil {
			t.Fatalf("daemon flag %q is missing", name)
		}
	}
}

func TestRunRequiresCredentialBeforeConfigurationOrState(t *testing.T) {
	clearDaemonOperatorEnvironment(t)
	err := run(context.Background(), daemonOptions{configPath: filepath.Join(t.TempDir(), "missing.yaml")})
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("run without operator token error = %v", err)
	}
}

func TestOperatorEnvironmentIsScrubbed(t *testing.T) {
	t.Setenv(operatorauth.TokenEnvironment, "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")
	t.Setenv(operatorauth.TokenFileEnvironment, "unused-token-file")
	t.Setenv("AUTORESEARCH_UNRELATED", "retained")
	scrubOperatorEnvironment()
	for _, name := range []string{operatorauth.TokenEnvironment, operatorauth.TokenFileEnvironment} {
		if _, present := os.LookupEnv(name); present {
			t.Fatalf("sensitive environment variable %s remains set", name)
		}
	}
	if os.Getenv("AUTORESEARCH_UNRELATED") != "retained" {
		t.Fatal("environment scrubbing removed unrelated configuration")
	}
}

func TestNonLoopbackWarningRequiresTLS(t *testing.T) {
	clearDaemonOperatorEnvironment(t)
	t.Setenv(operatorauth.TokenEnvironment, "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8")
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	blockingFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), daemonOptions{
		configPath:       "../../configs/autoresearch.yaml",
		listen:           "0.0.0.0:0",
		dataDir:          filepath.Join(blockingFile, "private-data"),
		allowNonLoopback: true,
	})
	if err == nil {
		t.Fatal("run unexpectedly initialized state beneath a regular file")
	}
	if !strings.Contains(logs.String(), "TLS") || strings.Contains(logs.String(), "without built-in authentication") {
		t.Fatalf("non-loopback warning is not TLS-focused: %s", logs.String())
	}
}

func clearDaemonOperatorEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{operatorauth.TokenEnvironment, operatorauth.TokenFileEnvironment} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}
