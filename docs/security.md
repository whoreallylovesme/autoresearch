# Security Model

## Trust boundaries

The operator and deterministic runtime policy are trusted within their configured scope. Models, agent backends, papers, web content, repositories, datasets, tool output, and experiment code are untrusted. Provider credentials, SSH keys, GitHub tokens, host files, other workspaces, and production infrastructure are outside the agent boundary.

## Autonomy boundary

Before a run, the operator defines tools, filesystem roots, executors, network destinations, budgets, concurrency, remote resources, and publication policy. Routine permitted actions need no approval. Actions outside that boundary require explicit operator authorization or are rejected.

## Controls

- Strict tool schemas and role allowlists.
- Dedicated least-privilege provider credentials and secret redaction.
- OS-level workload isolation and resource limits, not prompt-based restrictions.
- Default-deny network and no host Docker socket in workloads.
- Per-agent workspaces and artifact checksums.
- API authentication for non-loopback deployments and CSRF-safe operator controls.
- Immutable audit events and emergency stop.

## Human directives

Chat is informational; directives are authoritative structured operations with operator identity, validation, and audit events. A model cannot promote a chat message into a policy change. Budget and executor changes require runtime validation and, when configured, separate confirmation.

## Publication

The public repository contains only generic source, examples, and documentation. Research reports, datasets, experimental code, and logs remain private unless the operator explicitly exports and reviews them. Automated public pushes of research artifacts are prohibited.

## Limitations

No sandbox eliminates all kernel, supply-chain, side-channel, provider, or remote-host risk. The runtime cannot prove that model-generated research is correct. Operators remain responsible for disclosure, licensing, source terms, credential rotation, and independent scientific review.
