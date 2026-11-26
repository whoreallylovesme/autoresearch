# Multi-agent Development

AutoResearch permits flexible multi-agent development tactics. There is no repository-specific cap on agent count, delegation waves, workflow size, or choice of orchestration pattern. Selecting a role-based workflow, a large AgentSwarm, or a Tower mission does not require separate approval from this project.

This is a delegation policy for developing AutoResearch, not a claim that infrastructure has unlimited capacity. Platform capabilities, model quotas, execution budgets, permissions, security boundaries, and verification requirements still apply. This document cannot enable a mode or override a host restriction.

## Building blocks

| Pattern | Appropriate use | Ownership and integration |
| --- | --- | --- |
| Single `coder` | One coherent change with a clear file boundary | The agent implements and verifies its scoped change |
| `plan` + `explore` + `coder` roles | Architecture, independent discovery, implementation, and review | One integrator reconciles findings and owns shared files |
| AgentSwarm | Many independent read-only investigations or non-conflicting work items | Each item has a distinct scope; the parent synthesizes results |
| Tower | Repository-level missions with a base branch, worktrees, review gates, and background coordination | Mission changes are reviewed before integration; Tower state remains in `.tower/` |
| Multi-wave workflow | Discovery → implementation → verification → review → final integration | Each wave consumes verified outputs from the previous one |

Role names describe responsibilities, not necessarily new built-in agent types. For example, a `plan` agent can be the architect, an `explore` agent can be an independent critic, and a `coder` can be the implementer. Custom agent profiles may add specialized roles when the workflow benefits from them.

## Example workflows

### Parallel read-only audit

Use independent `explore` scopes:

1. Provider/configuration contracts and validation.
2. Scheduler, persistence, and failure recovery.
3. Web, Python, and browser acceptance.
4. Docker, remote execution, and security boundaries.

Agents make no edits. The parent deduplicates findings, checks evidence against the code, and assigns any resulting fixes to non-overlapping owners. Overlapping reads are acceptable; overlapping writes are not.

### Role-based feature implementation

1. A `plan` architect defines the smallest vertical slice, public contracts, and acceptance scenario.
2. Independent `coder` agents own disjoint modules or separate worktrees.
3. An `explore` critic reviews the integrated diff read-only, while another agent examines failure modes and test coverage.
4. A designated integrator resolves shared API/configuration files, applies review fixes, runs the complete relevant test suite, and updates documentation.

A backend agent and frontend agent may work in parallel only after their API contract is fixed. Shared protocol, migration, and public contract files should have one owner rather than receiving concurrent edits.

### Large multi-wave program

A program larger than one swarm is split into waves, for example:

1. **Map:** explore all affected packages and external contracts.
2. **Design:** compare implementation options and record ADRs where necessary.
3. **Implement:** independent coders or Tower missions work in isolated scopes.
4. **Verify:** run package, race, integration, browser, and deployment checks on the actual deliverable.
5. **Review:** independent security, correctness, reproducibility, and documentation reviews.
6. **Integrate:** one owner resolves conflicts and reruns checks after every material change.

There is no project-imposed number of waves or agents. Additional waves are normal when new evidence exposes more independent work. A wave should not start dependent work before its inputs are stable.

### Tower missions

Use Tower when repository-level mission tracking, isolated worktrees, review gates, and a stable integration branch are useful. It is experimental and is enabled by the operator with `/tower on`; `/tower status` inspects the current roster and missions.

Run Tower only from a session attached to the actual Git repository on the designated development machine. Do not initialize it in an empty or unrelated local directory. `/tower off` does not stop existing background agents or remove `.tower/` state; cleanup and integration are separate decisions.

## Platform boundaries

The freedom to choose tactics is not an absence of technical limits:

- AgentSwarm requires user-enabled swarm mode. Use `/swarm on` or send `/swarm <task>`; it accepts at most **128 items per call**. Larger programs use additional waves.
- Tower requires user activation and a Git repository. Its worktree, review, and cleanup boundaries must remain intact.
- Built-in `coder`, `explore`, and `plan` subagents do not recursively dispatch further agents by default. Deeper delegation requires a custom profile with an explicit `subagents` allowlist.
- Every agent has an isolated context and consumes model resources independently. Provide complete handoff inputs; do not assume it sees the parent conversation.
- Permission inheritance and tool allowlists continue to apply. Auto permission mode is not a license to bypass provider terms, secret isolation, execution policies, or plan review.
- Paid API calls and subscription-backed research prompts are not a mechanism for testing agent scale. Development verification uses deterministic mocks and credential-free acceptance tests.

Official product references: [Agents and Sub-Agents](https://www.kimi.com/code/docs/en/kimi-code-cli/customization/agents.html), [Slash Commands](https://www.kimi.com/code/docs/en/kimi-code-cli/reference/slash-commands.html), and [Configuration files](https://moonshotai.github.io/kimi-code/en/configuration/config-files.html).

## Shared-state and integration rules

- Give each writer a unique package, file set, or worktree; avoid duplicate investigations unless independent confirmation is intentional.
- Never run concurrent Git mutations or deployments in the same checkout. The integrator owns final commits, pushes, and merges.
- Preserve dirty worktrees, running agents, user changes, and private research state. Do not clean them up merely because a workflow ended.
- Verify the final integrated artifact, not only individual agent reports. A subagent's success message is not test evidence.
- Record completed work, unresolved failures, architectural decisions, and next steps in `docs/progress.md`.
- Do not publish credentials, private infrastructure addresses, research artifacts, session logs, or agent wire files.

The goal is not to maximize agent count for its own sake. It is to use as much parallelism as the task can support while keeping ownership, evidence, recovery, and scientific verification deterministic.
