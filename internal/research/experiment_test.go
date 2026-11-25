package research_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/experiment"
	"github.com/whoreallylovesme/autoresearch/internal/graph"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func TestAutonomousReproducibleExperiment(t *testing.T) {
	requireExperimentDocker(t)
	configuration, err := config.Load("../../configs/experiment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	configuration.Runtime.DataDir = t.TempDir()
	runtime, err := research.NewPersistent(configuration, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	experiments, err := runtime.Experiments(context.Background())
	if err != nil || len(experiments) != 1 {
		t.Fatalf("experiments = %#v, %v", experiments, err)
	}
	record := experiments[0]
	if record.Status != experiment.StatusVerified || record.Metrics["answer"] != 6 || record.Verification == nil || !record.Verification.Passed || len(record.Artifacts) == 0 || record.CodeSHA256 == "" {
		t.Fatalf("experiment = %#v", record)
	}
	hypothesis, ok := findGraphNode(runtime.Graph(), "hypothesis-deterministic-sum")
	if !ok || hypothesis.Status != graph.StatusVerified || len(hypothesis.EvidenceRefs) == 0 {
		t.Fatalf("hypothesis = %#v, %v", hypothesis, ok)
	}
	experimentNode, ok := findGraphNode(runtime.Graph(), record.ID)
	if !ok || experimentNode.Status != string(experiment.StatusVerified) || experimentNode.Revision < 2 {
		t.Fatalf("experiment node = %#v", experimentNode)
	}
	if !strings.Contains(runtime.Report(), "Experiments and verification") {
		t.Fatalf("report = %s", runtime.Report())
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := research.NewPersistent(configuration, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	experiments, err = reopened.Experiments(context.Background())
	if err != nil || len(experiments) != 1 || experiments[0].Status != experiment.StatusVerified {
		t.Fatalf("reopened experiments = %#v, %v", experiments, err)
	}
}

func requireExperimentDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker unavailable")
	}
	if err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		t.Skip("Docker daemon unavailable")
	}
}

func findGraphNode(snapshot graph.Snapshot, id string) (graph.Node, bool) {
	for _, node := range snapshot.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return graph.Node{}, false
}
