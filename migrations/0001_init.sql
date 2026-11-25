CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    state TEXT NOT NULL,
    started_at INTEGER NOT NULL DEFAULT 0,
    ended_at INTEGER NOT NULL DEFAULT 0,
    report TEXT NOT NULL DEFAULT '',
    configuration_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE TABLE tasks (
    run_id TEXT NOT NULL REFERENCES runs(id),
    id TEXT NOT NULL,
    state TEXT NOT NULL,
    idempotency_key TEXT NOT NULL DEFAULT '',
    data_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (run_id, id)
);
CREATE UNIQUE INDEX tasks_idempotency ON tasks(run_id, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX tasks_ready ON tasks(run_id, state);

CREATE TABLE events (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    sequence INTEGER NOT NULL,
    type TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    actor TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    UNIQUE (run_id, sequence)
);
CREATE INDEX events_type ON events(run_id, type, sequence);

CREATE TABLE findings (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    task_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    role TEXT NOT NULL,
    kind TEXT NOT NULL,
    summary TEXT NOT NULL,
    evidence_json TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX findings_task ON findings(run_id, task_id);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    sender TEXT NOT NULL,
    recipient TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    timestamp INTEGER NOT NULL,
    type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    UNIQUE (run_id, idempotency_key)
);
CREATE INDEX messages_recipient ON messages(run_id, recipient, timestamp);

CREATE TABLE budget_snapshots (
    run_id TEXT NOT NULL REFERENCES runs(id),
    id INTEGER NOT NULL,
    status_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (run_id, id)
);

CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    path TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    media_type TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX artifacts_run ON artifacts(run_id, sha256);
