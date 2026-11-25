package research

import (
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/config"
)

func loadConfigForDirective(t *testing.T) *config.Config {
	t.Helper()
	configuration, err := config.Load("../../configs/autoresearch.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return configuration
}
