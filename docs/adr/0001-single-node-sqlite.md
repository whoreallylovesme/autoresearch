# ADR 0001: Start with a single-node Go runtime and SQLite WAL

Status: Accepted

## Context

The initial deployment is one VPS. Correctness, recovery, and operability matter more than distributed scale.

## Decision

Use Go, SQLite WAL, migrations, transactional snapshots, and filesystem artifacts. Keep the API and interfaces clean without introducing external infrastructure.

## Consequences

Backup and recovery are simple and transactional. Horizontal scale and multiple active writers are out of scope until measured requirements justify a new ADR.
