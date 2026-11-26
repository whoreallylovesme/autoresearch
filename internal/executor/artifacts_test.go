package executor

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
)

func TestArtifactCollectionEnforcesRecordCountBoundary(t *testing.T) {
	collection := artifactCollection{store: newArtifactTestStore(t), runID: "run"}
	for index := 0; index < 20; index++ {
		if err := collection.put(context.Background(), "artifact", 0, artifactTestReader(nil)); err != nil {
			t.Fatalf("put %d: %v", index+1, err)
		}
	}
	if len(collection.records) != 20 {
		t.Fatalf("record count = %d, want 20", len(collection.records))
	}
	if err := collection.put(context.Background(), "artifact", 0, artifactTestReader(nil)); err == nil {
		t.Fatal("21st artifact was accepted")
	}
	if len(collection.records) != 20 {
		t.Fatalf("record count after rejection = %d, want 20", len(collection.records))
	}
}

func TestArtifactCollectionEnforcesSizeBoundaries(t *testing.T) {
	content := bytes.Repeat([]byte{'x'}, 16<<20)
	collection := artifactCollection{store: newArtifactTestStore(t), runID: "run"}
	for index := 0; index < 4; index++ {
		if err := collection.put(context.Background(), "artifact", int64(len(content)), artifactTestReader(content)); err != nil {
			t.Fatalf("put %d at size boundary: %v", index+1, err)
		}
	}
	if collection.total != 64<<20 {
		t.Fatalf("aggregate size = %d, want %d", collection.total, int64(64<<20))
	}
	if err := collection.put(context.Background(), "overflow", 1, artifactTestReader([]byte{'x'})); err == nil {
		t.Fatal("artifact exceeding the 64 MiB aggregate limit was accepted")
	}

	oversized := artifactCollection{store: newArtifactTestStore(t), runID: "run"}
	if err := oversized.put(context.Background(), "oversized", 16<<20+1, artifactTestReader(nil)); err == nil {
		t.Fatal("artifact exceeding the 16 MiB file limit was accepted")
	}
	if len(oversized.records) != 0 || oversized.total != 0 {
		t.Fatalf("rejected artifact changed accounting: records = %d, total = %d", len(oversized.records), oversized.total)
	}
}

func TestArtifactCollectionPropagatesStorePutErrors(t *testing.T) {
	collection := artifactCollection{store: newArtifactTestStore(t), runID: "run"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := collection.put(ctx, "artifact", 1, artifactTestReader([]byte{'x'})); !errors.Is(err, context.Canceled) {
		t.Fatalf("put error = %v, want context.Canceled", err)
	}
	if len(collection.records) != 0 || collection.total != 0 {
		t.Fatalf("failed put changed accounting: records = %d, total = %d", len(collection.records), collection.total)
	}
}

func newArtifactTestStore(t *testing.T) *artifact.Store {
	t.Helper()
	store, err := artifact.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func artifactTestReader(content []byte) func() ([]byte, error) {
	return func() ([]byte, error) {
		return content, nil
	}
}
