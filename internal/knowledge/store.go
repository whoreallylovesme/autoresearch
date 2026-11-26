package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/id"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

const (
	ClaimFromPaper      = "paper_claim"
	ClaimInterpretation = "interpretation"
	ClaimVerified       = "verified_conclusion"
	ClaimNegative       = "negative_result"
)

type Paper struct {
	ID                string    `json:"id"`
	RunID             string    `json:"run_id"`
	Source            string    `json:"source"`
	SourceID          string    `json:"source_id"`
	DOI               string    `json:"doi,omitempty"`
	ArxivID           string    `json:"arxiv_id,omitempty"`
	Title             string    `json:"title"`
	Abstract          string    `json:"abstract,omitempty"`
	Authors           []string  `json:"authors"`
	PublishedAt       time.Time `json:"published_at,omitempty"`
	URL               string    `json:"url,omitempty"`
	PDFURL            string    `json:"pdf_url,omitempty"`
	PDFArtifactID     string    `json:"pdf_artifact_id,omitempty"`
	FullText          string    `json:"full_text,omitempty"`
	SourceAttribution string    `json:"source_attribution"`
	FetchedAt         time.Time `json:"fetched_at"`
}

type Claim struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	PaperID   string    `json:"paper_id"`
	Type      string    `json:"type"`
	Text      string    `json:"text"`
	Page      string    `json:"page,omitempty"`
	Section   string    `json:"section,omitempty"`
	Evidence  []string  `json:"evidence,omitempty"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	runID string
	db    *sql.DB
}

func (s *Store) RunID() string { return s.runID }

func New(runID string, store *storage.SQLite) *Store {
	return &Store{runID: runID, db: store.DB()}
}

func (s *Store) AddPaper(ctx context.Context, paper Paper) (Paper, bool, error) {
	if paper.RunID != s.runID || paper.Source == "" || paper.SourceID == "" || paper.Title == "" || paper.SourceAttribution == "" {
		return Paper{}, false, errors.New("paper run, source, source ID, title, and attribution are required")
	}
	paper.DOI = normalizeDOI(paper.DOI)
	paper.ArxivID = normalizeArxiv(paper.ArxivID)
	if paper.ID == "" {
		digest := sha256.Sum256([]byte(paper.Source + "\x00" + paper.SourceID))
		paper.ID = "paper-" + hex.EncodeToString(digest[:12])
	}
	if paper.FetchedAt.IsZero() {
		paper.FetchedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Paper{}, false, err
	}
	defer tx.Rollback()
	existing, err := findDuplicate(ctx, tx, paper)
	if err == nil {
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Paper{}, false, err
	}
	authors, err := json.Marshal(paper.Authors)
	if err != nil {
		return Paper{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO papers(id, run_id, source, source_id, doi, arxiv_id, title, abstract, authors_json, published_at, url, pdf_url, pdf_artifact_id, full_text, source_attribution, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, paper.ID, paper.RunID, paper.Source, paper.SourceID, paper.DOI, paper.ArxivID, paper.Title, paper.Abstract, authors, paper.PublishedAt.UnixNano(), paper.URL, paper.PDFURL, paper.PDFArtifactID, paper.FullText, paper.SourceAttribution, paper.FetchedAt.UnixNano())
	if err != nil {
		return Paper{}, false, err
	}
	if err := insertSearch(ctx, tx, paper, nil); err != nil {
		return Paper{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Paper{}, false, err
	}
	return paper, false, nil
}

func (s *Store) AddClaim(ctx context.Context, claim Claim) error {
	_, err := s.AddClaimRecord(ctx, claim)
	return err
}

func (s *Store) AddClaimRecord(ctx context.Context, claim Claim) (Claim, error) {
	if claim.RunID != s.runID || claim.PaperID == "" || claim.Text == "" || claim.Author == "" {
		return Claim{}, errors.New("claim run, paper, text, and author are required")
	}
	if claim.Type != ClaimFromPaper && claim.Type != ClaimInterpretation && claim.Type != ClaimVerified && claim.Type != ClaimNegative {
		return Claim{}, errors.New("unsupported claim type")
	}
	if claim.Type == ClaimFromPaper && claim.Page == "" && claim.Section == "" {
		return Claim{}, errors.New("paper claims require a page or section location")
	}
	if claim.Type == ClaimVerified && len(claim.Evidence) == 0 {
		return Claim{}, errors.New("verified conclusions require verification evidence")
	}
	if claim.ID == "" {
		claim.ID = id.MustNew()
	}
	if claim.CreatedAt.IsZero() {
		claim.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Claim{}, err
	}
	defer tx.Rollback()
	paper, err := getPaper(ctx, tx, s.runID, claim.PaperID)
	if err != nil {
		return Claim{}, err
	}
	evidence, err := json.Marshal(claim.Evidence)
	if err != nil {
		return Claim{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO paper_claims(id, run_id, paper_id, type, text, page, section, evidence_json, author, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", claim.ID, claim.RunID, claim.PaperID, claim.Type, claim.Text, claim.Page, claim.Section, evidence, claim.Author, claim.CreatedAt.UnixNano()); err != nil {
		return Claim{}, err
	}
	if err := rebuildSearch(ctx, tx, paper); err != nil {
		return Claim{}, err
	}
	if err := tx.Commit(); err != nil {
		return Claim{}, err
	}
	return claim, nil
}

func (s *Store) AttachFullText(ctx context.Context, paperID, artifactID, fullText string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	paper, err := getPaper(ctx, tx, s.runID, paperID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE papers SET pdf_artifact_id = ?, full_text = ? WHERE id = ?", artifactID, fullText, paperID); err != nil {
		return err
	}
	paper.PDFArtifactID = artifactID
	paper.FullText = fullText
	if err := rebuildSearch(ctx, tx, paper); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Search(ctx context.Context, query string, limit int) ([]Paper, error) {
	if strings.TrimSpace(query) == "" || limit < 1 || limit > 50 {
		return nil, errors.New("search requires a query and limit between 1 and 50")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT paper_id FROM papers_fts WHERE run_id = ? AND papers_fts MATCH ? ORDER BY rank LIMIT ?", s.runID, ftsQuery(query), limit)
	if err != nil {
		return nil, err
	}
	ids, err := paperIDs(rows)
	if err != nil {
		return nil, err
	}
	return s.papersByIDs(ctx, ids)
}

func (s *Store) ListPapers(ctx context.Context) ([]Paper, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM papers WHERE run_id = ? ORDER BY title", s.runID)
	if err != nil {
		return nil, err
	}
	ids, err := paperIDs(rows)
	if err != nil {
		return nil, err
	}
	return s.papersByIDs(ctx, ids)
}

func (s *Store) Paper(ctx context.Context, paperID string) (Paper, error) {
	return getPaper(ctx, s.db, s.runID, paperID)
}

func (s *Store) Claims(ctx context.Context, paperID string) ([]Claim, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, run_id, paper_id, type, text, page, section, evidence_json, author, created_at FROM paper_claims WHERE run_id = ? AND paper_id = ? ORDER BY created_at, id", s.runID, paperID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var claims []Claim
	for rows.Next() {
		var claim Claim
		var evidence []byte
		var created int64
		if err := rows.Scan(&claim.ID, &claim.RunID, &claim.PaperID, &claim.Type, &claim.Text, &claim.Page, &claim.Section, &evidence, &claim.Author, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(evidence, &claim.Evidence); err != nil {
			return nil, err
		}
		claim.CreatedAt = time.Unix(0, created).UTC()
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func (s *Store) papersByIDs(ctx context.Context, ids []string) ([]Paper, error) {
	papers := make([]Paper, 0, len(ids))
	for _, id := range ids {
		paper, err := getPaper(ctx, s.db, s.runID, id)
		if err != nil {
			return nil, err
		}
		papers = append(papers, paper)
	}
	return papers, nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const paperColumns = "id, run_id, source, source_id, doi, arxiv_id, title, abstract, authors_json, published_at, url, pdf_url, pdf_artifact_id, full_text, source_attribution, fetched_at"

func getPaper(ctx context.Context, querier rowQuerier, runID, paperID string) (Paper, error) {
	return scanPaper(querier.QueryRowContext(ctx, "SELECT "+paperColumns+" FROM papers WHERE run_id = ? AND id = ?", runID, paperID))
}

func findDuplicate(ctx context.Context, tx *sql.Tx, paper Paper) (Paper, error) {
	return scanPaper(tx.QueryRowContext(ctx, `SELECT `+paperColumns+` FROM papers WHERE run_id = ? AND ((source = ? AND source_id = ?) OR (doi <> '' AND doi = ?) OR (arxiv_id <> '' AND arxiv_id = ?)) LIMIT 1`, paper.RunID, paper.Source, paper.SourceID, paper.DOI, paper.ArxivID))
}

func scanPaper(row *sql.Row) (Paper, error) {
	var paper Paper
	var authors []byte
	var published, fetched int64
	err := row.Scan(&paper.ID, &paper.RunID, &paper.Source, &paper.SourceID, &paper.DOI, &paper.ArxivID, &paper.Title, &paper.Abstract, &authors, &published, &paper.URL, &paper.PDFURL, &paper.PDFArtifactID, &paper.FullText, &paper.SourceAttribution, &fetched)
	if err != nil {
		return Paper{}, err
	}
	if err := json.Unmarshal(authors, &paper.Authors); err != nil {
		return Paper{}, err
	}
	if published > 0 {
		paper.PublishedAt = time.Unix(0, published).UTC()
	}
	paper.FetchedAt = time.Unix(0, fetched).UTC()
	return paper, nil
}

func rebuildSearch(ctx context.Context, tx *sql.Tx, paper Paper) error {
	rows, err := tx.QueryContext(ctx, "SELECT text FROM paper_claims WHERE run_id = ? AND paper_id = ?", paper.RunID, paper.ID)
	if err != nil {
		return err
	}
	var claims []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			rows.Close()
			return err
		}
		claims = append(claims, text)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM papers_fts WHERE run_id = ? AND paper_id = ?", paper.RunID, paper.ID); err != nil {
		return err
	}
	return insertSearch(ctx, tx, paper, claims)
}

func insertSearch(ctx context.Context, tx *sql.Tx, paper Paper, claims []string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO papers_fts(run_id, paper_id, title, abstract, full_text, claims) VALUES (?, ?, ?, ?, ?, ?)", paper.RunID, paper.ID, paper.Title, paper.Abstract, paper.FullText, strings.Join(claims, "\n"))
	return err
}

func paperIDs(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func ftsQuery(query string) string {
	words := strings.Fields(query)
	for index, word := range words {
		words[index] = `"` + strings.ReplaceAll(word, `"`, `""`) + `"`
	}
	return strings.Join(words, " AND ")
}

func normalizeDOI(doi string) string {
	doi = strings.ToLower(strings.TrimSpace(doi))
	doi = strings.TrimPrefix(doi, "https://doi.org/")
	doi = strings.TrimPrefix(doi, "http://doi.org/")
	doi = strings.TrimPrefix(doi, "doi:")
	return doi
}

func normalizeArxiv(arxiv string) string {
	arxiv = strings.ToLower(strings.TrimSpace(arxiv))
	arxiv = strings.TrimPrefix(arxiv, "arxiv:")
	return arxiv
}

func (p Paper) String() string {
	return fmt.Sprintf("%s (%s:%s)", p.Title, p.Source, p.SourceID)
}
