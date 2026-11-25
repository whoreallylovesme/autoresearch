CREATE TABLE graph_nodes (
    run_id TEXT NOT NULL REFERENCES runs(id),
    id TEXT NOT NULL,
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    revision INTEGER NOT NULL,
    data_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (run_id, id)
);
CREATE INDEX graph_nodes_filter ON graph_nodes(run_id, type, status);

CREATE TABLE graph_node_revisions (
    run_id TEXT NOT NULL REFERENCES runs(id),
    node_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    author TEXT NOT NULL,
    data_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (run_id, node_id, revision)
);

CREATE TABLE graph_edges (
    run_id TEXT NOT NULL REFERENCES runs(id),
    id TEXT NOT NULL,
    from_node TEXT NOT NULL,
    to_node TEXT NOT NULL,
    type TEXT NOT NULL,
    data_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (run_id, id)
);
CREATE INDEX graph_edges_nodes ON graph_edges(run_id, from_node, to_node);

CREATE TABLE papers (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    source TEXT NOT NULL,
    source_id TEXT NOT NULL,
    doi TEXT NOT NULL DEFAULT '',
    arxiv_id TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    abstract TEXT NOT NULL DEFAULT '',
    authors_json TEXT NOT NULL DEFAULT '[]',
    published_at INTEGER NOT NULL DEFAULT 0,
    url TEXT NOT NULL DEFAULT '',
    pdf_url TEXT NOT NULL DEFAULT '',
    pdf_artifact_id TEXT NOT NULL DEFAULT '',
    full_text TEXT NOT NULL DEFAULT '',
    source_attribution TEXT NOT NULL,
    fetched_at INTEGER NOT NULL,
    UNIQUE(run_id, source, source_id)
);
CREATE UNIQUE INDEX papers_doi ON papers(run_id, doi) WHERE doi <> '';
CREATE UNIQUE INDEX papers_arxiv ON papers(run_id, arxiv_id) WHERE arxiv_id <> '';

CREATE VIRTUAL TABLE papers_fts USING fts5(
    run_id,
    paper_id UNINDEXED,
    title,
    abstract,
    full_text,
    claims
);

CREATE TABLE paper_claims (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    paper_id TEXT NOT NULL REFERENCES papers(id),
    type TEXT NOT NULL,
    text TEXT NOT NULL,
    page TEXT NOT NULL DEFAULT '',
    section TEXT NOT NULL DEFAULT '',
    evidence_json TEXT NOT NULL DEFAULT '[]',
    author TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX paper_claims_paper ON paper_claims(run_id, paper_id, type);
