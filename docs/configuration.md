# Configuration

Configuration is YAML, versioned, strictly decoded, resolved relative to the referring file, and snapshotted before a run. Unknown fields, duplicate mapping keys, invalid references, missing prompts, cyclic includes, unsupported parameters, invalid permissions, impossible budgets, and dependency cycles are startup errors.

## Files

`configs/autoresearch.yaml` composes agent definitions, one task, budgets, and executors. Model configuration is separate from agent configuration. Prompts are Markdown files outside the configuration tree. Examples with real providers are intentionally non-active and require operator-selected, currently supported model identifiers.

## Model contract

For a mixed OpenAI-Chief/Kimi-worker fleet, copy `configs/mixed-providers.example.yaml`, set the actual Astra model identifier and current prices, and export `OPENAI_API_KEY` plus `MOONSHOT_API_KEY`. Worker model instances are independent but share endpoint-level rate limits. The Kimi example uses the currently documented `kimi-k3.1-pro` identifier and `reasoning_effort: high`.


Fields include `provider`, `model`, `base_url`, `api_key_env`, `parameters`, `capabilities`, `retry`, `timeout`, `rate_limit`, `pricing`, `estimation`, and provider-specific options. Real provider runs require per-million-token prices and `estimation.max_input_tokens`; prices are snapshotted and confirmed cost remains separate from reserved estimates. Secrets are environment-variable names or secret-store references, never inline values. `base_url_env` optionally overrides a model endpoint for container networks without putting credentials in YAML. Provider adapters declare accepted parameter names; an unsupported configured parameter is an error rather than silently ignored.

Do not infer capabilities from a model name. Validate them against official provider documentation and explicit configuration. `max_output_tokens` maps to the adapter's supported request field. Reasoning effort and temperature are omitted unless both configured and supported.

## Agent contract

An agent definition selects a role, model reference, prompt, tool allowlist, context policy, permissions, and optional resource limits. Roles are behavior and tool policy, not separate infrastructure privileges. The Chief Scientist cannot grant itself a tool or change executor policy.

## Task contract

Tasks reference objective, completion criteria, restrictions, deliverables, and verification configuration. Paths resolve relative to the task file. Natural-language criteria guide the supervisor; machine-verifiable criteria gate objective completion.

## Budget contract

Monetary, token, request, runtime, parallelism, disk, CPU, and GPU dimensions are independent. Soft, hard, and finalization reserves must be non-negative and ordered. Zero-cost mock runs may use zero monetary limits while retaining request limits.

## Snapshots

A snapshot contains canonical YAML, SHA-256 hashes for every referenced file, the runtime version, and creation time. Secrets and environment values are excluded. Restarts use the original snapshot rather than silently adopting edited configuration.

## Example

The active example under `configs/` is deterministic and free. Run validation before every launch:

```bash
researchctl validate --config configs/autoresearch.yaml
researchctl run --config configs/autoresearch.yaml
```

See `docs/progress.md` for command availability.
