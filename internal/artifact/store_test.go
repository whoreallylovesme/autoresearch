package artifact_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
)

func TestStoreWritesReadsAndDeduplicatesContent(t *testing.T) {
	store, err := artifact.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("# Report\nverified evidence\n")
	first, err := store.Put(context.Background(), "run-1", "../report.md", "text/markdown", content)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(context.Background(), "run-1", "other.md", "text/markdown", content)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 || first.StoragePath != second.StoragePath || first.Path != "report.md" {
		t.Fatalf("records = %#v, %#v", first, second)
	}
	read, err := store.Get(first)
	if err != nil || !bytes.Equal(read, content) {
		t.Fatalf("Get() = %q, %v", read, err)
	}
	if first.SizeBytes != int64(len(content)) || first.CreatedAt.IsZero() {
		t.Fatalf("record = %#v", first)
	}
}

func TestStoreDetectsTampering(t *testing.T) {
	store, err := artifact.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Put(context.Background(), "run-1", "result.json", "application/json", []byte(`{"metric":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(record.StoragePath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record.StoragePath, []byte(`{"metric":2}`), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(record); !errors.Is(err, artifact.ErrChecksumMismatch) {
		t.Fatalf("Get() error = %v", err)
	}
}
