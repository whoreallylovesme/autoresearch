package graph_test

import (
	"errors"
	"reflect"
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

func TestPublicAndPersistenceSnapshotsUseDeterministicOrdering(t *testing.T) {
	g := graph.New()
	var persisted graph.Snapshot
	g.SetPersistence(func(snapshot graph.Snapshot) error {
		persisted = snapshot
		return nil
	})
	for _, id := range []string{"node-c", "node-a", "node-b"} {
		if err := g.AddNode(graph.Node{ID: id, RunID: "run", Type: graph.NodeObjective, Author: "author", Description: "description"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, edge := range []graph.Edge{
		{ID: "edge-c", RunID: "run", FromNode: "node-b", ToNode: "node-c", Type: graph.EdgeSupports, Author: "author"},
		{ID: "edge-a", RunID: "run", FromNode: "node-a", ToNode: "node-c", Type: graph.EdgeSupports, Author: "author"},
		{ID: "edge-b", RunID: "run", FromNode: "node-a", ToNode: "node-b", Type: graph.EdgeSupports, Author: "author"},
	} {
		if err := g.AddEdge(edge); err != nil {
			t.Fatal(err)
		}
	}

	snapshot := g.Snapshot()
	for _, tc := range []struct {
		name  string
		nodes []graph.Node
		edges []graph.Edge
	}{
		{name: "public reads", nodes: g.Nodes(), edges: g.Edges()},
		{name: "public snapshot", nodes: snapshot.Nodes, edges: snapshot.Edges},
		{name: "persistence snapshot", nodes: persisted.Nodes, edges: persisted.Edges},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := graphNodeIDs(tc.nodes), []string{"node-a", "node-b", "node-c"}; !reflect.DeepEqual(got, want) {
				t.Errorf("node IDs = %v, want %v", got, want)
			}
			if got, want := graphEdgeIDs(tc.edges), []string{"edge-a", "edge-b", "edge-c"}; !reflect.DeepEqual(got, want) {
				t.Errorf("edge IDs = %v, want %v", got, want)
			}
		})
	}
}

func TestNodeSlicesAreIsolatedAcrossPublicReadsAndPersistenceSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(*graph.Graph, *graph.Snapshot) graph.Node
	}{
		{name: "Node", read: func(g *graph.Graph, _ *graph.Snapshot) graph.Node {
			node, _ := g.Node("node")
			return node
		}},
		{name: "Nodes", read: func(g *graph.Graph, _ *graph.Snapshot) graph.Node {
			return g.Nodes()[0]
		}},
		{name: "Revisions", read: func(g *graph.Graph, _ *graph.Snapshot) graph.Node {
			return g.Revisions("node")[0].Node
		}},
		{name: "Snapshot nodes", read: func(g *graph.Graph, _ *graph.Snapshot) graph.Node {
			return g.Snapshot().Nodes[0]
		}},
		{name: "Snapshot revisions", read: func(g *graph.Graph, _ *graph.Snapshot) graph.Node {
			return g.Snapshot().Revisions[0].Node
		}},
		{name: "persistence nodes", read: func(_ *graph.Graph, snapshot *graph.Snapshot) graph.Node {
			return snapshot.Nodes[0]
		}},
		{name: "persistence revisions", read: func(_ *graph.Graph, snapshot *graph.Snapshot) graph.Node {
			return snapshot.Revisions[0].Node
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := graph.New()
			var persisted graph.Snapshot
			g.SetPersistence(func(snapshot graph.Snapshot) error {
				persisted = snapshot
				return nil
			})
			if err := g.AddNode(graph.Node{
				ID: "node", RunID: "run", Type: graph.NodeObjective, Author: "author", Description: "description",
				RelatedTasks: []string{"task-original"}, Artifacts: []string{"artifact-original"}, EvidenceRefs: []string{"evidence-original"},
			}); err != nil {
				t.Fatal(err)
			}

			node := tc.read(g, &persisted)
			node.RelatedTasks[0] = "task-mutated"
			node.Artifacts[0] = "artifact-mutated"
			node.EvidenceRefs[0] = "evidence-mutated"

			assertOriginalGraphNodeSlices(t, g.Nodes()[0])
			assertOriginalGraphNodeSlices(t, g.Revisions("node")[0].Node)
			snapshot := g.Snapshot()
			assertOriginalGraphNodeSlices(t, snapshot.Nodes[0])
			assertOriginalGraphNodeSlices(t, snapshot.Revisions[0].Node)
		})
	}
}

func TestRestoreIsolatesRevisionNodeSlicesFromSourceSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(*testing.T, *graph.Graph) graph.Node
	}{
		{name: "Revisions", read: func(_ *testing.T, g *graph.Graph) graph.Node {
			return g.Revisions("node")[0].Node
		}},
		{name: "Snapshot", read: func(_ *testing.T, g *graph.Graph) graph.Node {
			return g.Snapshot().Revisions[0].Node
		}},
		{name: "persistence snapshot", read: func(t *testing.T, g *graph.Graph) graph.Node {
			var persisted graph.Snapshot
			g.SetPersistence(func(snapshot graph.Snapshot) error {
				persisted = snapshot
				return nil
			})
			if err := g.AddNode(graph.Node{ID: "zz-trigger", RunID: "run", Type: graph.NodeObjective, Author: "author", Description: "description"}); err != nil {
				t.Fatal(err)
			}
			return persisted.Revisions[0].Node
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := graph.New()
			if err := source.AddNode(graph.Node{
				ID: "node", RunID: "run", Type: graph.NodeObjective, Author: "author", Description: "description",
				RelatedTasks: []string{"task-original"}, Artifacts: []string{"artifact-original"}, EvidenceRefs: []string{"evidence-original"},
			}); err != nil {
				t.Fatal(err)
			}
			snapshot := source.Snapshot()
			restored := graph.New()
			if err := restored.Restore(snapshot); err != nil {
				t.Fatal(err)
			}

			snapshot.Revisions[0].Node.RelatedTasks[0] = "task-mutated"
			snapshot.Revisions[0].Node.Artifacts[0] = "artifact-mutated"
			snapshot.Revisions[0].Node.EvidenceRefs[0] = "evidence-mutated"

			assertOriginalGraphNodeSlices(t, tc.read(t, restored))
		})
	}
}

func graphNodeIDs(nodes []graph.Node) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func graphEdgeIDs(edges []graph.Edge) []string {
	ids := make([]string, 0, len(edges))
	for _, edge := range edges {
		ids = append(ids, edge.ID)
	}
	return ids
}

func assertOriginalGraphNodeSlices(t *testing.T, node graph.Node) {
	t.Helper()
	if got, want := node.RelatedTasks, []string{"task-original"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RelatedTasks = %v, want %v", got, want)
	}
	if got, want := node.Artifacts, []string{"artifact-original"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Artifacts = %v, want %v", got, want)
	}
	if got, want := node.EvidenceRefs, []string{"evidence-original"}; !reflect.DeepEqual(got, want) {
		t.Errorf("EvidenceRefs = %v, want %v", got, want)
	}
}
