package config_test

import (
	"path/filepath"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/config"
)

func TestScaffoldCreatesValidProjectAndRefusesOverwrite(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "project")
	if err := config.Scaffold(directory); err != nil {
		t.Fatal(err)
	}
	configuration, err := config.Load(filepath.Join(directory, "configs", "autoresearch.yaml"))
	if err != nil {
		t.Fatalf("generated configuration is invalid: %v", err)
	}
	if configuration.Name != "autoresearch-project" || len(configuration.Agents) != 3 {
		t.Fatalf("configuration = %#v", configuration)
	}
	if err := config.Scaffold(directory); err == nil {
		t.Fatal("second init overwrote an existing project")
	}
}
