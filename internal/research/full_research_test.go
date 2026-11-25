package research_test

import (
	"context"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/experiment"
	"github.com/whoreallylovesme/autoresearch/internal/graph"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func TestFullAutonomousResearchSession(t *testing.T) {
	requireExperimentDocker(t)
	configuration, err := config.Load("../../configs/full-research.yaml")
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
		t.Fatal(err)
	}
	papers, err := runtime.Papers(context.Background())
	if err != nil || len(papers) != 2 {
		t.Fatalf("papers = %#v, %v", papers, err)
	}
	experiments, err := runtime.Experiments(context.Background())
	if err != nil || len(experiments) != 1 || experiments[0].Status != experiment.StatusVerified {
		t.Fatalf("experiments = %#v, %v", experiments, err)
	}
	nodes := runtime.Graph().Nodes
	statuses := map[string]string{}
	for _, node := range nodes {
		statuses[node.ID] = node.Status
	}
	if statuses["hypothesis-deterministic-sum"] != graph.StatusVerified || statuses["hypothesis-multi-agent-coordination"] != graph.StatusProposed {
		t.Fatalf("hypothesis statuses = %#v", statuses)
	}
	playback, err := runtime.Playback(0)
	if err != nil || playback.ResearchState != research.StateCompleted || len(playback.Events) == 0 || playback.Report == "" {
		t.Fatalf("playback = %#v, %v", playback, err)
	}
}
