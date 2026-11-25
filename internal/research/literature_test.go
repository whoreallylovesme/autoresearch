package research_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/graph"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func TestLiteratureReviewProducesPersistentCitedGraph(t *testing.T) {
	configuration, err := config.Load("../../configs/literature.yaml")
	if err != nil {
		t.Fatal(err)
	}
	configuration.Runtime.DataDir = t.TempDir()
	runtime, err := research.NewPersistent(configuration, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	papers, err := runtime.Papers(context.Background())
	if err != nil || len(papers) != 2 {
		t.Fatalf("papers = %#v, %v", papers, err)
	}
	report := runtime.Report()
	for _, expected := range []string{"arXiv:2308.08155, Abstract", "arXiv:2303.17760, Abstract", "metadata"} {
		if !strings.Contains(strings.ToLower(report), strings.ToLower(expected)) {
			t.Fatalf("report does not contain %q:\n%s", expected, report)
		}
	}
	assertScienceGraph(t, runtime.Graph())
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := research.NewPersistent(configuration, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Status().State != research.StateCompleted {
		t.Fatalf("reopened state = %s", reopened.Status().State)
	}
	papers, err = reopened.Papers(context.Background())
	if err != nil || len(papers) != 2 {
		t.Fatalf("reopened papers = %#v, %v", papers, err)
	}
	assertScienceGraph(t, reopened.Graph())
}

func assertScienceGraph(t *testing.T, snapshot graph.Snapshot) {
	t.Helper()
	counts := map[graph.NodeType]int{}
	citations := 0
	hypothesis := false
	for _, node := range snapshot.Nodes {
		counts[node.Type]++
		if node.Type == graph.NodeHypothesis && node.Status == graph.StatusProposed {
			hypothesis = true
		}
	}
	for _, edge := range snapshot.Edges {
		if edge.Type == graph.EdgeCites {
			citations++
		}
	}
	if counts[graph.NodePaper] != 2 || counts[graph.NodeClaim] != 2 || counts[graph.NodeConclusion] != 1 || citations != 2 || !hypothesis {
		t.Fatalf("graph counts = %#v, citations = %d, hypothesis = %v; snapshot = %#v", counts, citations, hypothesis, snapshot)
	}
	if len(snapshot.Revisions) < len(snapshot.Nodes) {
		t.Fatalf("revision history is incomplete: %#v", snapshot.Revisions)
	}
}
