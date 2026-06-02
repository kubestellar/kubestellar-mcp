# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Command reference template documentation (commands/TEMPLATE.md) for consistent new command documentation
- Multi-client MCP setup documentation for VS Code, Cursor, Windsurf, and generic clients
- Troubleshooting section to README
- Architecture documentation
- Integration test guidance to CONTRIBUTING.md
- Unit tests for core MCP tools, handlers, CLI commands, and diagnostic components
- Coverage threshold gate to CI pipeline
- Environment Variables documentation in README

### Changed
- Dynamic API discovery implementation instead of static maps for GVR and scope resolution
- Kubernetes client libraries upgraded from EOL v0.30.4 to v0.36.1
- Go version consistently pinned across go.mod, Dockerfile, and CI workflows
- Refactored RESTMapper usage for dynamic GVR resolution instead of hardcoded kindToResource maps
- GitHub Actions updated to Node.js 24-compatible versions
- Workflow references pinned to commit SHA instead of @main branches (security improvement)

### Fixed
- Apply methods, path traversal vulnerabilities, tempDir leak, and resource kinds handling (#156)
- Nil panic in scaleApp and bound goroutine fan-out (#155)
- Nil pointer panic in findAppInCluster for nil Spec.Replicas (#134)
- Cluster discovery source handling (#135)
- Context propagation in kubectl_apply and helm tools
- Context propagation in GitOps ReadFromGit (SSRF vulnerability)
- GET error misclassification in sync and drift detection
- Health client timeout application
- Security issues: type assertions, injection, path traversal, and error handling (#105)
- Dockerfile base images with SHA digests security enhancement
- SSRF and context propagation in GitOps ReadFromGit
- Node.js 20 deprecation warnings

### Security
- Pinned workflow references to commit SHA and restricted secrets:inherit usage (#138)
- Updated golang.org/x/net from v0.23.0 to v0.55.0 (14 CVEs addressed) (#67)
- Added USER nonroot directive to Dockerfile (#100)
- Type assertion, injection, and path traversal vulnerability fixes (#105)

## [0.8.21] - 2025-05-31

### Changed
- GitHub Actions updated to Node.js 24-compatible versions

## [0.8.20] - 2025-05-15

### Added
- Homebrew availability guidance for kubectl-claude

## [0.8.19] - 2025-04-17

### Added
- kubestellar-deploy CLI usage documentation to README

## [0.8.18] - 2025-05-09

### Added
- Multi-client MCP setup docs for VS Code, Cursor, and Windsurf

## [0.8.17] - 2025-04-07

### Fixed
- Cluster discovery source handling
- Nil pointer panic in findAppInCluster

## [0.8.16] - 2025-04-21

### Changed
- Dynamic API discovery instead of static maps for GVR and scope

## [0.8.15] - 2025-04-17

### Added
- README troubleshooting section
- Multi-client MCP setup docs

### Security
- Removed secrets:inherit and added fork guards to workflows

## [0.8.14] - 2025-04-07

### Added
- MCP setup instructions for VS Code, Cursor, Windsurf, and generic clients
- Architecture documentation
- golangci-lint CI gate

## [0.8.13] - 2025-03-31

### Added
- Integration test guidance to CONTRIBUTING.md
- Coverage threshold gate to CI pipeline

## [0.8.12]

### Added
- Environment Variables documentation
- Unit tests for CLI commands and MCP tools

### Fixed
- Kubernetes client libraries upgrade from v0.30.4 to v0.36.1

## [0.8.11]

### Added
- Go version pinning consistency across go.mod, Dockerfile, and CI

### Fixed
- Type assertions and path traversal security issues

## [0.8.10]

### Fixed
- GET error misclassification in sync and drift detection

## [0.8.9]

### Fixed
- SSRF and context propagation in GitOps ReadFromGit
- Context propagation in kubectl_apply and helm tools

## [0.8.8]

### Fixed
- Health client timeout application

### Security
- Updated golang.org/x/net (14 CVEs)

## [0.8.7]

### Added
- Unit tests for core MCP tools and handlers

## [0.8.6]

### Security
- Added USER nonroot directive to Dockerfile

## [0.8.5]

### Changed
- RESTMapper for dynamic GVR resolution

## [0.8.4]

### Changed
- Go version aligned to 1.22 across build configurations

## [0.8.3]

### Fixed
- Node.js 20 deprecation warnings
- Go version pinning consistency

## [0.8.2]

### Added
- CONTRIBUTING.md and RBAC guidance
- Documentation structure

## [0.8.1]

### Added
- Initial MCP (Model Context Protocol) tool implementations

## [0.8.0] - 2024

### Added
- Core MCP server implementation
- Kubernetes cluster integration tools
- Multi-cluster support
- GitOps integration
- Helm support
- Basic CLI commands

[Unreleased]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.21...HEAD
[0.8.21]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.20...v0.8.21
[0.8.20]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.19...v0.8.20
[0.8.19]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.18...v0.8.19
[0.8.18]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.17...v0.8.18
[0.8.17]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.16...v0.8.17
[0.8.16]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.15...v0.8.16
[0.8.15]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.14...v0.8.15
[0.8.14]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.13...v0.8.14
[0.8.13]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.12...v0.8.13
[0.8.12]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.11...v0.8.12
[0.8.11]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.10...v0.8.11
[0.8.10]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.9...v0.8.10
[0.8.9]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.8...v0.8.9
[0.8.8]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.7...v0.8.8
[0.8.7]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.6...v0.8.7
[0.8.6]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.5...v0.8.6
[0.8.5]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.4...v0.8.5
[0.8.4]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.3...v0.8.4
[0.8.3]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.2...v0.8.3
[0.8.2]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.1...v0.8.2
[0.8.1]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/kubestellar/kubestellar-mcp/releases/tag/v0.8.0
