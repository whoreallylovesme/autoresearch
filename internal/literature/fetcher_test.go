package literature_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/knowledge"
	"github.com/whoreallylovesme/autoresearch/internal/literature"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

func TestArxivSourceParsesAtomAndAttribution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.URL.RawQuery, "multi-agent") || request.Header.Get("User-Agent") == "" {
			t.Errorf("request = %s, headers = %#v", request.URL.String(), request.Header)
		}
		writer.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(writer, `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom" xmlns:arxiv="http://arxiv.org/schemas/atom"><entry><id>http://arxiv.org/abs/2308.08155v1</id><updated>2023-08-16T00:00:00Z</updated><published>2023-08-16T00:00:00Z</published><title>AutoGen</title><summary>Multi-agent framework.</summary><author><name>Qingyun Wu</name></author><arxiv:doi>10.48550/arXiv.2308.08155</arxiv:doi><link title="pdf" href="https://arxiv.org/pdf/2308.08155" rel="related" type="application/pdf"/></entry></feed>`)
	}))
	defer server.Close()
	source := literature.NewArxivSource(server.Client(), server.URL)
	papers, err := source.Search(context.Background(), "multi-agent", 1)
	if err != nil || len(papers) != 1 {
		t.Fatalf("Search() = %#v, %v", papers, err)
	}
	paper := papers[0]
	if paper.Source != "arxiv" || paper.SourceID != "2308.08155v1" || paper.DOI != "10.48550/arxiv.2308.08155" || paper.PDFURL == "" || paper.SourceAttribution == "" {
		t.Fatalf("paper = %#v", paper)
	}
}

func TestFetcherSharesConcurrentSearchCache(t *testing.T) {
	sqlite, knowledgeStore, _ := testStores(t, "run")
	defer sqlite.Close()
	source := &countingSource{paper: knowledge.Paper{Source: "static", SourceID: "one", Title: "Multi-Agent Research", SourceAttribution: "static"}}
	fetcher := literature.New(knowledgeStore, nil, nil, source)
	const callers = 12
	var wait sync.WaitGroup
	var sourceCalls atomic.Int32
	source.calls = &sourceCalls
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			papers, err := fetcher.Search(context.Background(), "multi-agent", 5)
			if err != nil || len(papers) != 1 {
				t.Errorf("Search() = %#v, %v", papers, err)
			}
		}()
	}
	wait.Wait()
	if sourceCalls.Load() != 1 {
		t.Fatalf("source calls = %d", sourceCalls.Load())
	}
}

func TestFetcherDeduplicatesPDFAndStoresFullText(t *testing.T) {
	sqlite, knowledgeStore, artifacts := testStores(t, "run")
	defer sqlite.Close()
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		writer.Write([]byte("%PDF-1.4\nmock"))
	}))
	defer server.Close()
	processor := literature.PDFProcessor{Command: fakePDFCommand(t), Timeout: time.Second}
	fetcher := literature.New(knowledgeStore, artifacts, &processor, nil)
	paper, _, err := knowledgeStore.AddPaper(context.Background(), knowledge.Paper{RunID: "run", Source: "static", SourceID: "pdf", Title: "PDF Paper", PDFURL: server.URL, SourceAttribution: "static"})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 6 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			updated, err := fetcher.FetchPDF(context.Background(), paper)
			if err != nil || !strings.Contains(updated.FullText, "extracted full text") {
				t.Errorf("FetchPDF() = %#v, %v", updated, err)
			}
		}()
	}
	wait.Wait()
	if downloads.Load() != 1 {
		t.Fatalf("downloads = %d", downloads.Load())
	}
	results, err := knowledgeStore.Search(context.Background(), "xylophone", 5)
	if err != nil || len(results) != 1 || results[0].PDFArtifactID == "" {
		t.Fatalf("full text search = %#v, %v", results, err)
	}
}

type countingSource struct {
	paper knowledge.Paper
	calls *atomic.Int32
}

func (s *countingSource) Name() string { return "static" }

func (s *countingSource) Search(_ context.Context, _ string, _ int) ([]knowledge.Paper, error) {
	s.calls.Add(1)
	time.Sleep(10 * time.Millisecond)
	return []knowledge.Paper{s.paper}, nil
}

func testStores(t *testing.T, runID string) (*storage.SQLite, *knowledge.Store, *artifact.Store) {
	t.Helper()
	sqlite, err := storage.Open(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SaveRun(context.Background(), storage.Run{ID: runID, Name: runID, State: "running"}, nil); err != nil {
		t.Fatal(err)
	}
	artifacts, err := artifact.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	return sqlite, knowledge.New(runID, sqlite), artifacts
}

func fakePDFCommand(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pdftotext")
	content := []byte("#!/bin/sh\nprintf 'extracted full text xylophone'\n")
	if err := os.WriteFile(path, content, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArxivSourceReturnsStatusAfterRetryExhaustion(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	source := literature.NewArxivSource(server.Client(), server.URL)
	source.MinInterval = time.Millisecond
	_, err := source.Search(context.Background(), "test", 1)
	if err == nil || !strings.Contains(err.Error(), "500") || calls.Load() != 3 {
		t.Fatalf("Search() error = %v, calls = %d", err, calls.Load())
	}
}
