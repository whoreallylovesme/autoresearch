package literature

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/knowledge"
	"github.com/whoreallylovesme/autoresearch/internal/ratelimit"
)

type Source interface {
	Name() string
	Search(context.Context, string, int) ([]knowledge.Paper, error)
}

type ArxivSource struct {
	client      *http.Client
	baseURL     string
	pacer       ratelimit.Pacer
	MinInterval time.Duration
}

func NewArxivSource(client *http.Client, baseURL string) *ArxivSource {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://export.arxiv.org/api/query"
	}
	return &ArxivSource{client: client, baseURL: baseURL, MinInterval: 3 * time.Second}
}

func (s *ArxivSource) Name() string { return "arxiv" }

func (s *ArxivSource) Search(ctx context.Context, query string, limit int) ([]knowledge.Paper, error) {
	if strings.TrimSpace(query) == "" || limit < 1 || limit > 50 {
		return nil, errors.New("arXiv search requires a query and limit between 1 and 50")
	}
	values := url.Values{"search_query": {"all:" + query}, "start": {"0"}, "max_results": {fmt.Sprintf("%d", limit)}}
	endpoint := s.baseURL
	if strings.Contains(endpoint, "?") {
		endpoint += "&"
	} else {
		endpoint += "?"
	}
	endpoint += values.Encode()
	var response *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.wait(ctx); err != nil {
			return nil, err
		}
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if requestErr != nil {
			return nil, requestErr
		}
		request.Header.Set("User-Agent", "AutoResearch/0.1 scientific-literature-fetcher")
		response, err = s.client.Do(request)
		if err == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			break
		}
		if response != nil {
			response.Body.Close()
		}
		if err == nil && response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
			return nil, fmt.Errorf("arXiv returned HTTP %d", response.StatusCode)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("arXiv returned no response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("arXiv returned HTTP %d after retries", response.StatusCode)
	}
	defer response.Body.Close()
	var feed atomFeed
	if err := xml.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode arXiv Atom: %w", err)
	}
	papers := make([]knowledge.Paper, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		published, _ := time.Parse(time.RFC3339, entry.Published)
		pdfURL := ""
		for _, link := range entry.Links {
			if link.Title == "pdf" || link.Type == "application/pdf" {
				pdfURL = link.Href
			}
		}
		authors := make([]string, 0, len(entry.Authors))
		for _, author := range entry.Authors {
			authors = append(authors, author.Name)
		}
		papers = append(papers, knowledge.Paper{Source: "arxiv", SourceID: arxivID(entry.ID), ArxivID: arxivID(entry.ID), DOI: strings.ToLower(entry.DOI), Title: cleanSpace(entry.Title), Abstract: cleanSpace(entry.Summary), Authors: authors, PublishedAt: published, URL: entry.ID, PDFURL: pdfURL, SourceAttribution: "arXiv Atom API"})
	}
	return papers, nil
}

func (s *ArxivSource) wait(ctx context.Context) error {
	interval := s.MinInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return s.pacer.Wait(ctx, interval)
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string       `xml:"id"`
	Published string       `xml:"published"`
	Title     string       `xml:"title"`
	Summary   string       `xml:"summary"`
	Authors   []atomAuthor `xml:"author"`
	DOI       string       `xml:"doi"`
	Links     []atomLink   `xml:"link"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Href  string `xml:"href,attr"`
	Title string `xml:"title,attr"`
	Type  string `xml:"type,attr"`
}

func arxivID(raw string) string {
	raw = strings.TrimRight(raw, "/")
	parts := strings.Split(raw, "/")
	return parts[len(parts)-1]
}

func cleanSpace(value string) string { return strings.Join(strings.Fields(value), " ") }

type StaticSource struct {
	name   string
	papers []knowledge.Paper
}

func NewStaticSource(name string, papers []knowledge.Paper) *StaticSource {
	return &StaticSource{name: name, papers: append([]knowledge.Paper(nil), papers...)}
}

func (s *StaticSource) Name() string { return s.name }

func (s *StaticSource) Search(_ context.Context, query string, limit int) ([]knowledge.Paper, error) {
	if limit < 1 || limit > 50 {
		return nil, errors.New("limit must be between 1 and 50")
	}
	words := strings.Fields(strings.ToLower(query))
	var papers []knowledge.Paper
	for _, paper := range s.papers {
		text := strings.ToLower(paper.Title + " " + paper.Abstract + " " + paper.SourceID)
		matched := true
		for _, word := range words {
			if !strings.Contains(text, word) {
				matched = false
				break
			}
		}
		if matched {
			paper.Source = s.name
			paper.SourceAttribution = "Bundled deterministic bibliographic metadata"
			papers = append(papers, paper)
			if len(papers) == limit {
				break
			}
		}
	}
	return papers, nil
}
