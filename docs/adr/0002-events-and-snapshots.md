# ADR 0002: Combine append-only events with current-state snapshots

Status: Accepted

## Context

Audit and replay suggest event sourcing; scheduling and UI queries need efficient current state.

## Decision

Write current-state tables and their events atomically. Events carry per-run sequence numbers. Rebuild projections only when useful; do not reconstruct every scheduler decision from the full log.

## Consequences

Consumers get audit and live ordering while operational queries remain straightforward. Schema and invariants must keep both representations consistent.
