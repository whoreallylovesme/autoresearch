# ADR 0005: Separate deterministic tests from local HF inference

Status: Accepted

## Context

Tests need repeatable outputs and zero cost; the operator also requires a small Hugging Face model running on the VPS.

## Decision

Implement a scripted zero-cost `MockProvider` for correctness tests and an OpenAI-compatible local provider for Qwen2.5 0.5B GGUF served by llama.cpp. Use the local model for realistic smoke demonstrations, never as a deterministic oracle.

## Consequences

Tests remain stable while real HTTP inference is exercised locally. Model quality is explicitly limited and no scientific claim relies on the small model alone.
