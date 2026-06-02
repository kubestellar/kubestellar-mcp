## Security Policy

The KubeStellar community takes security reports seriously. If you believe you have found a vulnerability in `kubestellar-mcp`, please report it privately so maintainers can investigate and coordinate a fix before public disclosure.

`kubestellar-mcp` is a Kubernetes MCP (Model Context Protocol) server that can inspect cluster state, analyze RBAC, and help with deployment workflows. Security issues may affect Kubernetes credentials, cluster access, or the integrity of workloads managed through the tool.

## Supported Versions

We currently provide security fixes on a best-effort basis for the most recent development line and the latest stable release.

| Version | Supported |
| --- | --- |
| `main` branch / current nightly builds | ✅ Best effort |
| Latest stable release | ✅ |
| Older releases | ❌ |

If you are unsure whether your version is supported, report the issue anyway.

## Reporting a Vulnerability

**Preferred:** Open a private report through GitHub Security Advisories for this repository:

- https://github.com/kubestellar/kubestellar-mcp/security/advisories/new

**Alternative:** Email the private KubeStellar security contact:

- [kubestellar-security-announce@googlegroups.com](mailto:kubestellar-security-announce@googlegroups.com)

Please include as much of the following as possible:

- affected version, commit, or release tag
- environment details (Kubernetes version, client, OS)
- clear reproduction steps or proof of concept
- impact assessment
- any suggested mitigation or fix

Please **do not** open public GitHub issues for suspected vulnerabilities.

## Response Timeline

We aim to:

- acknowledge new reports within **3 business days**
- complete initial triage within **7 business days**
- provide periodic status updates as investigation and remediation progress

Response times may vary with report complexity and maintainer availability, but we will keep reporters informed throughout the process.

## Coordinated Disclosure

We follow coordinated disclosure practices aligned with common CNCF and Kubernetes community expectations:

- reports are handled privately while the issue is investigated
- maintainers may work with reporters on severity, scope, mitigations, and fix timing
- public disclosure should happen after a fix or reasonable mitigation is available, unless earlier disclosure is required to protect users

## What Qualifies as a Security Issue

Examples of issues that should be reported privately include:

- authentication or authorization bypass
- privilege escalation or unintended cluster access
- exposure of kubeconfig data, tokens, secrets, or other sensitive information
- arbitrary command execution or unsafe code execution paths
- SSRF, path traversal, injection, or similar vulnerabilities in MCP handlers or supporting code
- insecure defaults or workflow behavior that could materially compromise a Kubernetes cluster or its workloads

If a bug only affects correctness, usability, documentation, or feature behavior without security impact, please report it through the normal public issue tracker instead.
