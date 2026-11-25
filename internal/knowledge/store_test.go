package knowledge_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/knowledge"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

func TestKnowledgeDeduplicatesAndSearchesPapers(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	paper := knowledge.Paper{
		RunID: "run", Source: "arxiv", SourceID: "2308.08155", ArxivID: "2308.08155", DOI: "10.48550/arXiv.2308.08155",
		Title: "AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation", Abstract: "A framework for multi-agent conversation.",
		Authors: []string{"Qingyun Wu"}, PublishedAt: time.Date(2023, 8, 16, 0, 0, 0, 0, time.UTC), URL: "https://arxiv.org/abs/2308.08155", SourceAttribution: "arXiv",
	}
	created, duplicate, err := store.AddPaper(ctx, paper)
	if err != nil || duplicate {
		t.Fatalf("AddPaper() = %#v, %v, %v", created, duplicate, err)
	}
	duplicatePaper := paper
	duplicatePaper.Source = "crossref"
	duplicatePaper.SourceID = "doi-alternative"
	duplicatePaper.ID = ""
	existing, duplicate, err := store.AddPaper(ctx, duplicatePaper)
	if err != nil || !duplicate || existing.ID != created.ID {
		t.Fatalf("duplicate AddPaper() = %#v, %v, %v", existing, duplicate, err)
	}
	results, err := store.Search(ctx, "multi-agent conversation", 10)
	if err != nil || len(results) != 1 || results[0].ID != created.ID {
		t.Fatalf("Search() = %#v, %v", results, err)
	}
	claim := knowledge.Claim{RunID: "run", PaperID: created.ID, Type: knowledge.ClaimInterpretation, Text: "Conversation topology may improve agent coordination", Section: "Abstract", Author: "worker-1"}
	if err := store.AddClaim(ctx, claim); err != nil {
		t.Fatal(err)
	}
	results, err = store.Search(ctx, "coordination", 10)
	if err != nil || len(results) != 1 {
		t.Fatalf("claim Search() = %#v, %v", results, err)
	}
	if err := store.AttachFullText(ctx, created.ID, "artifact-pdf", "Section 4 describes an experimental evaluation with unique-token-xylophone."); err != nil {
		t.Fatal(err)
	}
	results, err = store.Search(ctx, "xylophone", 10)
	if err != nil || len(results) != 1 || results[0].PDFArtifactID != "artifact-pdf" {
		t.Fatalf("full text Search() = %#v, %v", results, err)
	}
	claims, err := store.Claims(ctx, created.ID)
	if err != nil || len(claims) != 1 || claims[0].Type != knowledge.ClaimInterpretation {
		t.Fatalf("Claims() = %#v, %v", claims, err)
	}
}

func TestKnowledgeSeparatesClaimsAndRequiresEvidence(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	paper, _, err := store.AddPaper(ctx, knowledge.Paper{RunID: "run", Source: "arxiv", SourceID: "1706.03762", Title: "Attention Is All You Need", SourceAttribution: "arXiv"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddClaim(ctx, knowledge.Claim{RunID: "run", PaperID: paper.ID, Type: knowledge.ClaimFromPaper, Text: "Exact claim", Author: "reader"}); err == nil {
		t.Fatal("paper claim without source location was accepted")
	}
	if err := store.AddClaim(ctx, knowledge.Claim{RunID: "run", PaperID: paper.ID, Type: knowledge.ClaimVerified, Text: "Verified", Section: "Experiment", Author: "model"}); err == nil {
		t.Fatal("verified claim without evidence was accepted")
	}
	if err := store.AddClaim(ctx, knowledge.Claim{RunID: "run", PaperID: paper.ID, Type: knowledge.ClaimVerified, Text: "Verified", Section: "Experiment", Evidence: []string{"verification-1"}, Author: "verifier"}); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "research.db")
	sqlite, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SaveRun(context.Background(), storage.Run{ID: "run", Name: "run", State: "running"}, nil); err != nil {
		t.Fatal(err)
	}
	store := knowledge.New("run", sqlite)
	_, _, err = store.AddPaper(context.Background(), knowledge.Paper{RunID: "run", Source: "arxiv", SourceID: "2303.17760", Title: "CAMEL", Abstract: "Communicative agents", SourceAttribution: "arXiv"})
	if err != nil {
		t.Fatal(err)
	}
	sqlite.Close()
	sqlite, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()
	store = knowledge.New("run", sqlite)
	results, err := store.Search(context.Background(), "communicative", 10)
	if err != nil || len(results) != 1 {
		t.Fatalf("Search() after reopen = %#v, %v", results, err)
	}
}

func newStore(t *testing.T) *knowledge.Store {
	t.Helper()
	sqlite, err := storage.Open(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlite.Close() })
	if err := sqlite.SaveRun(context.Background(), storage.Run{ID: "run", Name: "run", State: "running"}, nil); err != nil {
		t.Fatal(err)
	}
	return knowledge.New("run", sqlite)
}
