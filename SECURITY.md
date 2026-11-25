# Security Policy

AutoResearch executes model-proposed actions and is therefore security-sensitive infrastructure, not a trusted chat interface.

## Reporting

Report vulnerabilities privately through GitHub Security Advisories for this repository. Do not open a public issue containing exploit details, credentials, private infrastructure addresses, or unpublished research. Maintainers should acknowledge reports as resources permit and coordinate disclosure with the reporter.

## Operational requirements

Use dedicated low-privilege credentials, isolated workspaces, explicit network policy, resource limits, and an operator emergency stop. Never expose a host Docker socket to an agent. Treat papers, web pages, model output, and tool output as untrusted data. Public project development must remain separate from private research state.

The runtime cannot provide a security boundary by prompt engineering. Sandbox and executor guarantees must come from operating-system isolation.
