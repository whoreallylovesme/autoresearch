package id_test

import (
	"encoding/hex"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/id"
)

func TestNewProducesDistinct128BitHexIdentifiers(t *testing.T) {
	first, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("identifiers were repeated: %q", first)
	}
	for _, value := range []string{first, second} {
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != 16 {
			t.Fatalf("identifier %q is not 128-bit hex: %v", value, err)
		}
	}
}

func TestMustNewUsesTheSameRepresentation(t *testing.T) {
	value := id.MustNew()
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 16 {
		t.Fatalf("identifier %q is not 128-bit hex: %v", value, err)
	}
}
