package research_test

import (
	"context"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/graph"
	"github.com/whoreallylovesme/autoresearch/internal/research"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

func TestPlaybackReconstructsResearchTimeline(t *testing.T) {
	cfg := loadConfig(t)
	runtime, err := research.New(cfg, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatal(err)
	}
	early, err := runtime.Playback(2)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range early.Tasks {
		if task.State == scheduler.StateCompleted {
			t.Fatalf("task completed too early: %#v", task)
		}
	}
	latest, err := runtime.Playback(0)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ResearchState != research.StateCompleted || len(latest.Tasks) != 3 {
		t.Fatalf("latest = %#v", latest)
	}
	for _, task := range latest.Tasks {
		if task.State != scheduler.StateCompleted {
			t.Fatalf("latest task = %#v", task)
		}
	}
	foundConclusion := false
	for _, node := range latest.Graph.Nodes {
		if node.Type == graph.NodeConclusion {
			foundConclusion = true
		}
	}
	if !foundConclusion {
		t.Fatal("final conclusion is missing from playback")
	}
}
