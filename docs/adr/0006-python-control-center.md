# ADR 0006: Use a Python control center over the public runtime API

Status: Accepted

## Context

The operator needs rich dashboards and graph visualization without maintaining a separate TypeScript application.

## Decision

Use NiceGUI, Plotly, and Cytoscape.js. The web application and CLI share versioned REST/SSE contracts and never write runtime storage directly.

## Consequences

Iteration is fast and visualization libraries are accessible. API contract discipline is necessary to prevent duplicate control logic.
