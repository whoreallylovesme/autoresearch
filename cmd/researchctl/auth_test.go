package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthGenerateWritesPrivateTokenWithoutPrintingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator-token")
	root := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"auth", "generate", "--file", path})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) == 0 || bytes.Contains(output.Bytes(), bytes.TrimSpace(content)) {
		t.Fatal("auth generate printed the operator token")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("generated token permissions = %o", info.Mode().Perm())
	}

	before := append([]byte(nil), content...)
	root = newRootCommand()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"auth", "generate", "--file", path})
	if err := root.Execute(); err == nil {
		t.Fatal("auth generate overwrote an existing token file")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("existing token changed after generation failure")
	}
}

func TestAuthGenerateRequiresFile(t *testing.T) {
	root := newRootCommand()
	root.SetArgs([]string{"auth", "generate"})
	if err := root.Execute(); err == nil {
		t.Fatal("auth generate succeeded without --file")
	}
}
