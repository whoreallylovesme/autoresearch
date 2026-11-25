package config_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/config"
)

func TestLoadDemoResolvesConfigurationAndSnapshot(t *testing.T) {
	root := copyConfigs(t)
	cfg, err := config.Load(filepath.Join(root, "autoresearch.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Name != "deterministic-scheduling-demo" {
		t.Fatalf("Name = %q", cfg.Name)
	}
	if len(cfg.Agents) != 5 {
		t.Fatalf("len(Agents) = %d", len(cfg.Agents))
	}
	if cfg.Chief.Role != "chief-scientist" {
		t.Fatalf("Chief.Role = %q", cfg.Chief.Role)
	}
	if cfg.Task.Objective == "" || cfg.Task.CompletionCriteria == "" {
		t.Fatal("task text was not loaded")
	}
	if cfg.Runtime.MaxParallelTasks != 4 {
		t.Fatalf("MaxParallelTasks = %d", cfg.Runtime.MaxParallelTasks)
	}
	if len(cfg.Snapshot.Files) < 15 {
		t.Fatalf("snapshot has %d files, want all references", len(cfg.Snapshot.Files))
	}
	for path, sum := range cfg.Snapshot.Files {
		if !filepath.IsAbs(path) || len(sum) != 64 {
			t.Fatalf("invalid snapshot entry %q: %q", path, sum)
		}
	}
	encoded, err := json.Marshal(cfg.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), os.Getenv("HOME")) {
		t.Fatal("snapshot leaked an environment value")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	root := copyConfigs(t)
	path := filepath.Join(root, "autoresearch.yaml")
	appendFile(t, path, "\nunexpected_field: true\n")
	_, err := config.Load(path)
	assertErrorContains(t, err, "unexpected_field")
}

func TestLoadRejectsDuplicateKey(t *testing.T) {
	root := copyConfigs(t)
	path := filepath.Join(root, "autoresearch.yaml")
	appendFile(t, path, "\nname: duplicate\n")
	_, err := config.Load(path)
	assertErrorContains(t, err, "duplicate")
}

func TestLoadRejectsMissingPrompt(t *testing.T) {
	root := copyConfigs(t)
	if err := os.Remove(filepath.Join(root, "..", "prompts", "critic.md")); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(filepath.Join(root, "autoresearch.yaml"))
	assertErrorContains(t, err, "critic.md")
}

func TestLoadRejectsCircularModelReference(t *testing.T) {
	root := copyConfigs(t)
	path := filepath.Join(root, "agents", "chief-scientist.yaml")
	replaceInFile(t, path, "../models/mock.yaml", "chief-scientist.yaml")
	_, err := config.Load(filepath.Join(root, "autoresearch.yaml"))
	assertErrorContains(t, err, "circular")
}

func TestLoadRejectsZeroHeadcount(t *testing.T) {
	root := copyConfigs(t)
	path := filepath.Join(root, "autoresearch.yaml")
	replaceInFile(t, path, "headcount: 2", "headcount: 0")
	_, err := config.Load(filepath.Join(root, "autoresearch.yaml"))
	assertErrorContains(t, err, "headcount")
}

func TestLoadRejectsUnsupportedModelParameter(t *testing.T) {
	root := copyConfigs(t)
	path := filepath.Join(root, "models", "mock.yaml")
	replaceInFile(t, path, "  temperature: 0", "  unsupported_reasoning: maximal")
	_, err := config.Load(filepath.Join(root, "autoresearch.yaml"))
	assertErrorContains(t, err, "unsupported_reasoning")
}

func TestLoadRejectsIncompatibleTool(t *testing.T) {
	root := copyConfigs(t)
	path := filepath.Join(root, "agents", "baby-scientist.yaml")
	replaceInFile(t, path, "submit_finding, send_message", "submit_finding, finalize_research")
	_, err := config.Load(filepath.Join(root, "autoresearch.yaml"))
	assertErrorContains(t, err, "finalize_research")
}

func TestLoadRejectsInvalidBudgetOrdering(t *testing.T) {
	root := copyConfigs(t)
	path := filepath.Join(root, "budget.yaml")
	replaceInFile(t, path, "soft_limit_usd: 0", "soft_limit_usd: 2")
	_, err := config.Load(filepath.Join(root, "autoresearch.yaml"))
	assertErrorContains(t, err, "soft_limit_usd")
}

func copyConfigs(t *testing.T) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "configs")
	source := filepath.Join("..", "..", "configs")
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	promptsDestination := filepath.Join(destination, "..", "prompts")
	if err := os.MkdirAll(promptsDestination, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join("..", "..", "prompts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join("..", "..", "prompts", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(promptsDestination, entry.Name()), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return destination
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func replaceInFile(t *testing.T, path, old, replacement string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(content), old, replacement, 1)
	if updated == string(content) {
		t.Fatalf("%q not found in %s", old, path)
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertErrorContains(t *testing.T, err error, substring string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substring) {
		t.Fatalf("error = %v, want substring %q", err, substring)
	}
}
