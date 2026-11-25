package literature

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/knowledge"
)

type searchFlight struct {
	done   chan struct{}
	papers []knowledge.Paper
	err    error
}

type pdfFlight struct {
	done  chan struct{}
	paper knowledge.Paper
	err   error
}

type Fetcher struct {
	store       *knowledge.Store
	artifacts   *artifact.Store
	processor   *PDFProcessor
	sources     []Source
	client      *http.Client
	mu          sync.Mutex
	searches    map[string]*searchFlight
	searchCache map[string]cachedSearch
	pdfs        map[string]*pdfFlight
	hosts       map[string]time.Time
}

type cachedSearch struct {
	papers []knowledge.Paper
	at     time.Time
}

func New(store *knowledge.Store, artifacts *artifact.Store, processor *PDFProcessor, sources ...Source) *Fetcher {
	if processor == nil {
		processor = &PDFProcessor{Command: "pdftotext", Timeout: 30 * time.Second}
	}
	filtered := make([]Source, 0, len(sources))
	for _, source := range sources {
		if source != nil {
			filtered = append(filtered, source)
		}
	}
	return &Fetcher{store: store, artifacts: artifacts, processor: processor, sources: filtered, client: &http.Client{Timeout: 45 * time.Second}, searches: map[string]*searchFlight{}, searchCache: map[string]cachedSearch{}, pdfs: map[string]*pdfFlight{}, hosts: map[string]time.Time{}}
}

func (f *Fetcher) Search(ctx context.Context, query string, limit int) ([]knowledge.Paper, error) {
	if len(f.sources) == 0 || strings.TrimSpace(query) == "" || limit < 1 || limit > 50 {
		return nil, errors.New("paper search requires a source, query, and limit between 1 and 50")
	}
	key := fmt.Sprintf("%s\x00%d", strings.ToLower(strings.TrimSpace(query)), limit)
	f.mu.Lock()
	if cached, ok := f.searchCache[key]; ok && time.Since(cached.at) < 5*time.Minute {
		f.mu.Unlock()
		return append([]knowledge.Paper(nil), cached.papers...), nil
	}
	if flight, ok := f.searches[key]; ok {
		f.mu.Unlock()
		select {
		case <-flight.done:
			return flight.papers, flight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	flight := &searchFlight{done: make(chan struct{})}
	f.searches[key] = flight
	f.mu.Unlock()
	flight.papers, flight.err = f.search(ctx, query, limit)
	f.mu.Lock()
	delete(f.searches, key)
	if flight.err == nil {
		f.searchCache[key] = cachedSearch{papers: flight.papers, at: time.Now()}
	}
	close(flight.done)
	f.mu.Unlock()
	return flight.papers, flight.err
}

func (f *Fetcher) search(ctx context.Context, query string, limit int) ([]knowledge.Paper, error) {
	var papers []knowledge.Paper
	var errs []error
	for _, source := range f.sources {
		results, err := source.Search(ctx, query, limit)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", source.Name(), err))
			continue
		}
		for _, paper := range results {
			paper.RunID = f.store.RunID()
			if paper.Source == "" {
				paper.Source = source.Name()
			}
			if paper.SourceAttribution == "" {
				paper.SourceAttribution = source.Name()
			}
			stored, _, err := f.store.AddPaper(ctx, paper)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			papers = append(papers, stored)
			if len(papers) >= limit {
				return papers, errors.Join(errs...)
			}
		}
	}
	return papers, errors.Join(errs...)
}

func (f *Fetcher) FetchPDF(ctx context.Context, paper knowledge.Paper) (knowledge.Paper, error) {
	if paper.PDFArtifactID != "" {
		return paper, nil
	}
	if f.artifacts == nil {
		return knowledge.Paper{}, errors.New("artifact storage is not configured")
	}
	if _, err := url.ParseRequestURI(paper.PDFURL); err != nil {
		return knowledge.Paper{}, errors.New("paper has no valid PDF URL")
	}
	f.mu.Lock()
	if flight, ok := f.pdfs[paper.ID]; ok {
		f.mu.Unlock()
		select {
		case <-flight.done:
			return flight.paper, flight.err
		case <-ctx.Done():
			return knowledge.Paper{}, ctx.Err()
		}
	}
	flight := &pdfFlight{done: make(chan struct{})}
	f.pdfs[paper.ID] = flight
	f.mu.Unlock()
	flight.paper, flight.err = f.fetchPDF(ctx, paper)
	f.mu.Lock()
	delete(f.pdfs, paper.ID)
	close(flight.done)
	f.mu.Unlock()
	return flight.paper, flight.err
}

func (f *Fetcher) fetchPDF(ctx context.Context, paper knowledge.Paper) (knowledge.Paper, error) {
	if err := f.waitHost(ctx, paper.PDFURL); err != nil {
		return knowledge.Paper{}, err
	}
	var content []byte
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		var request *http.Request
		request, err = http.NewRequestWithContext(ctx, http.MethodGet, paper.PDFURL, nil)
		if err != nil {
			return knowledge.Paper{}, err
		}
		request.Header.Set("User-Agent", "AutoResearch/0.1 scientific-literature-fetcher")
		var response *http.Response
		response, err = f.client.Do(request)
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			content, err = io.ReadAll(io.LimitReader(response.Body, (32<<20)+1))
			response.Body.Close()
			if err == nil && len(content) <= 32<<20 {
				break
			}
			if len(content) > 32<<20 {
				err = errors.New("PDF exceeds 32 MiB")
			}
		} else {
			if response != nil {
				response.Body.Close()
				if response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
					return knowledge.Paper{}, fmt.Errorf("PDF source returned HTTP %d", response.StatusCode)
				}
			}
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return knowledge.Paper{}, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
			}
		}
	}
	if err != nil {
		return knowledge.Paper{}, err
	}
	if len(content) < 5 || string(content[:5]) != "%PDF-" {
		return knowledge.Paper{}, errors.New("downloaded artifact is not a PDF")
	}
	record, err := f.artifacts.Put(ctx, paper.RunID, paper.ID+".pdf", "application/pdf", content)
	if err != nil {
		return knowledge.Paper{}, err
	}
	text, err := f.processor.Extract(ctx, content)
	if err != nil {
		return knowledge.Paper{}, err
	}
	if err := f.store.AttachFullText(ctx, paper.ID, record.ID, string(text)); err != nil {
		return knowledge.Paper{}, err
	}
	updated, err := f.store.Paper(ctx, paper.ID)
	return updated, err
}

func (f *Fetcher) waitHost(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("PDF URL must use HTTP(S)")
	}
	f.mu.Lock()
	now := time.Now()
	next := f.hosts[parsed.Host]
	if next.Before(now) {
		next = now
	}
	f.hosts[parsed.Host] = next.Add(time.Second)
	f.mu.Unlock()
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
