# Scientific Reproducibility

**Implementation status:** Experiment records persist code/configuration hashes, seeds, executor/job details, hardware, dependencies when supplied, commands, logs, metrics, artifacts, and verification outcomes. Verified, failed, and refuted results are retained and exposed at `GET /api/v1/experiments`.

Every experiment record includes, when applicable, experiment and hypothesis IDs, source revision, Git commit, canonical configuration, dataset version and checksum, environment and dependency lock data, random seeds, hardware, executor, commands, metrics, logs, artifact checksums, and verification results.

## Immutable evidence

Negative, failed, interrupted, and inconclusive results are retained alongside successes. Historical result records are append-only; corrections reference earlier records. An agent cannot replace a metric or delete a contradictory artifact.

## Verification

Unit tests, benchmarks, statistical checks, Lean proofs, and custom commands implement a common verifier interface. Verification records contain criterion version, input checksums, execution evidence, and result. Model claims do not set objective verification status.

## Reporting

The final report separates established findings, experimentally supported findings, negative results, unverified hypotheses, limitations, open questions, and future work. Every citation and experimental claim links to durable provenance. Missing values are marked unavailable.

## Limits

A recorded seed does not guarantee bitwise reproducibility across hardware, drivers, model providers, or nondeterministic dependencies. The runtime records these factors and states the achieved reproduction scope rather than promising universal reproducibility.
