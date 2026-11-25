# Research Graph

**Implementation status:** The graph is implemented with typed nodes/edges, immutable revision snapshots, SQLite persistence, and verifier-gated hypothesis transitions. API consumers can read the live graph at `GET /api/v1/graph`.

The research graph is durable scientific state and a primary output, not a visualization derived from chat.

## Nodes and edges

Nodes include objectives, directions, hypotheses, claims, papers, evidence, experiments, benchmarks, counterexamples, proofs, refutations, artifacts, and conclusions. Edges include `depends_on`, `supports`, `contradicts`, `derived_from`, `investigates`, `verifies`, `refutes`, `implements`, and `cites`.

Every node has a stable ID, run ID, author, type, status, description, timestamps, related tasks, artifacts, evidence references, and immutable revision history. Edges also have provenance. Updates append revisions; historical evidence is not silently overwritten.

## Hypothesis status

`proposed → investigating → supported | refuted | inconclusive`, with `supported → verified` only after required verification. `abandoned` records a deliberate stop. A model can propose a transition, but only verifier evidence can set `verified`.

## Consistency rules

Node and edge types are validated, references must exist in the same research namespace, and self-contradictory transitions require a new revision and explanation. Multiple contradictory claims coexist with provenance. Deletion is represented as tombstoning, not history erasure.

## Queries and UI

Storage supports nodes by status, author, direction, and task; incident edges; evidence chains; and timeline snapshots. The API emits graph revision numbers so live clients can apply updates without losing ordering. Pan, zoom, filtering, and timeline are projections of this data.
