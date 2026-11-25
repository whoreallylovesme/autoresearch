package graph_test

import (
	"errors"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/graph"
)

func TestHypothesisVerificationRequiresObjectiveEvidence(t *testing.T) {
	g := graph.New()
	node := graph.Node{ID: "hypothesis-1", RunID: "run", Type: graph.NodeHypothesis, Author: "chief", Status: graph.StatusProposed, Description: "Scheduler improves reproducibility", CreatedAt: time.Now().UTC()}
	if err := g.AddNode(node); err != nil {
		t.Fatal(err)
	}
	if err := g.TransitionHypothesis(node.ID, graph.StatusInvestigating, nil); err != nil {
		t.Fatal(err)
	}
	if err := g.TransitionHypothesis(node.ID, graph.StatusSupported, nil); err != nil {
		t.Fatal(err)
	}
	if err := g.TransitionHypothesis(node.ID, graph.StatusVerified, nil); !errors.Is(err, graph.ErrVerificationRequired) {
		t.Fatalf("verification error = %v", err)
	}
	evidence := []graph.Verification{{ID: "verification-1", Verifier: "unit-test", Passed: true, Command: "go test ./...", ExitCode: 0, ArtifactIDs: []string{"artifact-1"}}}
	if err := g.TransitionHypothesis(node.ID, graph.StatusVerified, evidence); err != nil {
		t.Fatal(err)
	}
	verified, _ := g.Node(node.ID)
	if verified.Status != graph.StatusVerified || verified.Revision != 4 || len(verified.EvidenceRefs) != 1 {
		t.Fatalf("verified = %#v", verified)
	}
	if len(g.Revisions(node.ID)) != 4 {
		t.Fatalf("revisions = %d", len(g.Revisions(node.ID)))
	}
}

func TestGraphRejectsInvalidEdgesAndStatusTransitions(t *testing.T) {
	g := graph.New()
	for _, node := range []graph.Node{
		{ID: "paper", RunID: "run", Type: graph.NodePaper, Author: "fetcher", Status: "available", Description: "Paper"},
		{ID: "claim", RunID: "run", Type: graph.NodeClaim, Author: "worker", Status: "interpretation", Description: "Claim"},
	} {
		if err := g.AddNode(node); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.AddEdge(graph.Edge{ID: "edge", RunID: "run", FromNode: "claim", ToNode: "missing", Type: graph.EdgeCites}); err == nil {
		t.Fatal("edge to missing node was accepted")
	}
	if err := g.AddEdge(graph.Edge{ID: "edge", RunID: "run", FromNode: "claim", ToNode: "paper", Type: graph.EdgeCites, Author: "worker"}); err != nil {
		t.Fatal(err)
	}
	if err := g.TransitionHypothesis("claim", graph.StatusVerified, nil); err == nil {
		t.Fatal("non-hypothesis transition was accepted")
	}
}

func TestGraphSnapshotRoundTrip(t *testing.T) {
	g := graph.New()
	if err := g.AddNode(graph.Node{ID: "objective", RunID: "run", Type: graph.NodeObjective, Author: "operator", Status: "active", Description: "Objective"}); err != nil {
		t.Fatal(err)
	}
	var persisted graph.Snapshot
	g.SetPersistence(func(snapshot graph.Snapshot) error {
		persisted = snapshot
		return nil
	})
	if err := g.AddNode(graph.Node{ID: "hypothesis", RunID: "run", Type: graph.NodeHypothesis, Author: "chief", Status: graph.StatusProposed, Description: "Hypothesis"}); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Nodes) != 2 {
		t.Fatalf("snapshot = %#v", persisted)
	}
	restored := graph.New()
	if err := restored.Restore(persisted); err != nil {
		t.Fatal(err)
	}
	if len(restored.Nodes()) != 2 || len(restored.Revisions("hypothesis")) != 1 {
		t.Fatalf("restored = %#v", restored.Nodes())
	}
}

func TestReviseNodeAppendsHistory(t *testing.T) {
	g := graph.New()
	node := graph.Node{ID: "experiment", RunID: "run", Type: graph.NodeExperiment, Author: "chief", Status: "running", Description: "Experiment"}
	if err := g.AddNode(node); err != nil {
		t.Fatal(err)
	}
	node.Status = "verified"
	node.Artifacts = []string{"artifact-1"}
	if err := g.ReviseNode(node); err != nil {
		t.Fatal(err)
	}
	updated, _ := g.Node(node.ID)
	if updated.Status != "verified" || updated.Revision != 2 || len(g.Revisions(node.ID)) != 2 {
		t.Fatalf("updated = %#v", updated)
	}
}
