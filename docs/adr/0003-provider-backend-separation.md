# ADR 0003: Separate model providers from agent backends

Status: Accepted

## Context

A raw model API and a complete coding-agent harness have different lifecycle, event, and tool semantics.

## Decision

Define independent `Provider` and `AgentBackend` interfaces. Native agents compose a provider; external harnesses implement backend adapters through official structured protocols.

## Consequences

Models and harnesses remain replaceable. More adapter code is required, but interactive terminal emulation and false capability assumptions are avoided.
