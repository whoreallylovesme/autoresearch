CREATE TABLE experiments (
    run_id TEXT NOT NULL REFERENCES runs(id),
    id TEXT NOT NULL,
    hypothesis_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    executor_name TEXT NOT NULL,
    data_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (run_id, id)
);
CREATE INDEX experiments_status ON experiments(run_id, status);
CREATE INDEX experiments_hypothesis ON experiments(run_id, hypothesis_id);
