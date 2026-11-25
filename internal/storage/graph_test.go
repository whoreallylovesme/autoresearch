package storage_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/graph"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

func TestSQLiteGraphSnapshotRoundTrip(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveRun(ctx, storage.Run{ID: "run", Name: "run", State: "running"}, nil); err != nil {
		t.Fatal(err)
	}
	researchGraph := graph.New()
	if err := researchGraph.AddNode(graph.Node{ID: "paper", RunID: "run", Type: graph.NodePaper, Author: "fetcher", Status: "available", Description: "Paper"}); err != nil {
		t.Fatal(err)
	}
	if err := researchGraph.AddNode(graph.Node{ID: "claim", RunID: "run", Type: graph.NodeClaim, Author: "worker", Status: "interpretation", Description: "Claim"}); err != nil {
		t.Fatal(err)
	}
	if err := researchGraph.AddEdge(graph.Edge{ID: "cites", RunID: "run", FromNode: "claim", ToNode: "paper", Type: graph.EdgeCites, Author: "worker"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGraph(ctx, "run", researchGraph.Snapshot()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.LoadGraph(ctx, "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Nodes) != 2 || len(snapshot.Edges) != 1 || len(snapshot.Revisions) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	restored := graph.New()
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if len(restored.Nodes()) != 2 {
		t.Fatalf("restored = %#v", restored.Nodes())
	}
}
