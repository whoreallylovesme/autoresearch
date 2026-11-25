# Execution and Sandboxing

Executors implement submission, cancellation, status, logs, timeout, resource accounting, artifact collection, exit codes, and recovery. Implementations are local process, Docker, SSH, optional Slurm, and deterministic mock.

## Default local boundary

Untrusted workloads run without host credentials, with a dedicated workspace, unprivileged user, read-only root filesystem where practical, PID/cpu/memory/disk/time limits, dropped capabilities, no new privileges, seccomp/AppArmor where available, and default-deny network. The host Docker socket is never mounted.

Rootless Docker is preferred. Root-managed Docker may be an interim development equivalent only if containers themselves are unprivileged and tightly restricted; the distinction is documented in deployment status. Local process execution is not called a security sandbox unless an equivalent OS boundary is active.

## Workspaces

Concurrent coding agents receive separate Git worktrees or independent workspaces. Paths are canonicalized and checked against workspace roots. Symlink escapes, device mounts, sensitive environment variables, and artifact path traversal are rejected.

## Remote execution

SSH and Slurm use dedicated configured identities, host-key verification, allowlisted destinations, remote workspace roots, explicit environment, timeouts, cancellation, and resource requests. The runtime never assumes unrestricted remote access. Private keys remain in an operator-controlled secret store and are never included in prompts or artifacts.

## Artifacts and verification

Logs stream with sequence numbers and durable offsets. Artifacts are collected within count/size limits and stored by SHA-256. A verifier receives immutable inputs and emits command, revision, environment, timestamps, exit status, metrics, and evidence checksums.

## Emergency stop

The operator stop control revokes leases, cancels contexts and executor jobs, prevents new admissions, and preserves available state. Remote cancellation is best-effort and must be reconciled rather than assumed successful.
