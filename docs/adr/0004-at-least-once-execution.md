# ADR 0004: Use at-least-once dispatch with idempotency and fencing

Status: Accepted

## Context

A process can crash after an external operation but before recording its result. Exactly-once external execution cannot be assumed.

## Decision

Persist idempotency keys, lease owners, expiries, and fencing tokens. Recover and reconcile ambiguous operations before retrying. Duplicate completion of durable results is suppressed.

## Consequences

Some external operations may repeat. Tool and executor contracts must make this visible and support idempotent behavior wherever possible.
