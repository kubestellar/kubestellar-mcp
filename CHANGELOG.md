# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This changelog focuses on stable operator-facing releases. Nightly prereleases remain available in [GitHub Releases](https://github.com/kubestellar/kubestellar-mcp/releases).

## [Unreleased]

### Added
- Added `commands/TEMPLATE.md` to standardize future command reference documentation.
- Added operator documentation for multi-client MCP setup, `kubestellar-deploy` CLI usage, architecture, troubleshooting, environment variables, and integration testing.
- Added broader unit test coverage and ratcheted the repository coverage threshold.

### Changed
- Switched API discovery from static GVR maps to dynamic discovery and synced CI workflows from `kubestellar/infra`.

### Fixed
- Fixed apply-method handling, resource kind handling, path traversal checks, and the `tempDir` leak.
- Fixed nil panics in `scaleApp` and `findAppInCluster`, plus cluster discovery source handling.

### Security
- Pinned workflow references, removed `secrets:inherit`, and added fork guards for workflow execution.

## [v0.8.21] - 2026-05-31

### Added
- Expanded contributor and operator documentation, including README installation updates and RBAC guidance.
- Added broad unit test coverage across CLI commands, MCP handlers, deploy tooling, upgrades, and multi-cluster flows.

### Changed
- Pinned reusable workflow references and updated GitHub Actions to Node.js 24-compatible versions.
- Applied tighter timeout handling to health checks.

### Fixed
- Fixed context propagation in `kubectl_apply`, Helm tooling, and GitOps `ReadFromGit` flows.
- Fixed multiple security-related error handling and path validation issues.

### Security
- Upgraded `golang.org/x/net` to address known CVEs.
- Bumped Kubernetes client libraries from EOL `v0.30.4` to `v0.36.1`.
- Pinned Docker base images by digest and ran the image as a non-root user.

## [v0.8.20] - 2026-05-14

### Changed
- Retagged the `v0.8.19` codebase with no additional code changes.

## [v0.8.19] - 2026-05-14

### Changed
- Replaced hardcoded kind-to-resource mapping with dynamic RESTMapper-based GVR resolution.
- Aligned and pinned Go versions across `go.mod`, the Dockerfile, and CI workflows.

### Removed
- Removed unused `SyncOptions.Prune` and `DriftTypeExtra` paths.

### Fixed
- Fixed GET error classification in sync and drift detection flows.

## [v0.8.18] - 2026-05-08

### Added
- Added a Dockerfile for running and distributing the MCP server.

### Changed
- Synced repository workflows from `kubestellar/infra`.

## [v0.8.17] - 2026-04-25

### Changed
- Retagged the `v0.8.16` codebase with no additional code changes.

## [v0.8.16] - 2026-04-25

### Changed
- Disabled Copilot workflows to reduce GitHub API rate limiting.
- Removed the PR title emoji checker caller from CI automation.

## [v0.8.15] - 2026-04-16

### Fixed
- Fixed the MCP handshake to handle `notifications/initialized` correctly.

## [v0.8.14] - 2026-04-06

### Added
- Added the `label` command.

### Changed
- Added marketplace release automation.

## [v0.8.13] - 2026-03-31

### Changed
- Cleaned up `OWNERS` metadata by removing an invalid username entry.

## [v0.8.12] - 2026-02-11

### Changed
- Retagged the `v0.8.5` codebase with no additional code changes.

## [v0.8.11] - 2026-02-11

### Changed
- Retagged the `v0.8.5` codebase with no additional code changes.

## [v0.8.10] - 2026-02-11

### Changed
- Retagged the `v0.8.5` codebase with no additional code changes.

## [v0.8.9] - 2026-02-11

### Changed
- Retagged the `v0.8.5` codebase with no additional code changes.

## [v0.8.8] - 2026-02-11

### Changed
- Retagged the `v0.8.5` codebase with no additional code changes.

## [v0.8.7] - 2026-02-11

### Changed
- Retagged the `v0.8.5` codebase with no additional code changes.

## [v0.8.6] - 2026-02-11

### Changed
- Retagged the `v0.8.5` codebase with no additional code changes.

## [v0.8.5] - 2026-02-11

### Changed
- Clarified plugin setup documentation and added explicit `/plugin install` steps.

### Fixed
- Fixed a nil pointer crash in `get_pods` when `StartTime` is absent.

## [v0.8.4] - 2026-02-05

### Changed
- Retagged the `v0.8.3` codebase with no additional code changes.

## [v0.8.3] - 2026-02-05

### Changed
- Synced repository workflows from `kubestellar/infra`.

## [v0.8.2] - 2026-01-29

### Added
- Added nightly release automation for MCP binaries.

### Changed
- Renamed the binaries from `klaude-ops` and `klaude-deploy` to `kubestellar-ops` and `kubestellar-deploy`.
- Updated the Go module path to `kubestellar-mcp`.
- Synced CI workflows from `kubestellar/infra`.

### Removed
- Removed the redundant docs-release workflow.

## [v0.8.1] - 2026-01-16

### Added
- Added enhanced CRUD tooling to `klaude-deploy`.
- Added kustomize support and resource labeling support to deployment workflows.
- Added a build-and-test CI workflow.

### Changed
- Updated the README to document the expanded deploy tooling.

### Fixed
- Fixed lint failures and improved CI execution.

## [v0.8.0] - 2026-01-16

### Added
- Added the `detect_drift` GitOps drift-detection tool.

### Changed
- Added docs version branch triggering to the release workflow.
- Documented the new drift-detection workflow in the CLI docs.

### Fixed
- Fixed an unused import in `server.go`.

## [v0.7.1] - 2026-01-15

### Added
- Added a generic progress bar component.
- Added the `watch-upgrade` command.
- Added documentation for plugin setup, allowed-tools configuration, and upgrade watching.

## [v0.7.0] - 2026-01-15

### Added
- Added `klaude-deploy` and combined the project into a monorepo.

## [v0.6.0] - 2026-01-15

### Changed
- Renamed `kubectl-claude` to `klaude`.
- Added manual `workflow_dispatch` support to the docs-release workflow.

## [v0.5.0] - 2026-01-15

### Added
- Added upgrade tools and slash commands for cluster upgrades.

## [v0.4.3] - 2026-01-14

### Fixed
- Fixed the docs-release workflow JSON payload format.

## [v0.4.2] - 2026-01-14

### Fixed
- Fixed docs-release workflow `client_payload` parsing.

## [v0.4.1] - 2026-01-14

### Added
- Added a Quick Start section.
- Added a dedicated `docs/` directory for versioned documentation.
- Added docs-release automation support.

### Changed
- Updated the README for the expanded documentation set.

## [v0.4.0] - 2026-01-14

### Added
- Added resource ownership tooling.
- Added OPA Gatekeeper policy tooling.

## [v0.3.2] - 2026-01-14

### Added
- Enhanced `audit_kubeconfig` with kubeconfig consolidation suggestions.

## [v0.3.1] - 2026-01-14

### Fixed
- Fixed GoReleaser workflow handling for `HOMEBREW_TAP_TOKEN`.

## [v0.3.0] - 2026-01-14

### Added
- Added the `audit_kubeconfig` tool for kubeconfig cleanup.
- Added documentation for allowing MCP tools to run without repeated prompts.

## [v0.2.0] - 2026-01-14

### Added
- Added diagnostic tools for finding errors and misconfigurations.

## [v0.1.0] - 2026-01-14

### Added
- Initial implementation of the project.
- Added MCP server mode for Claude Code integration.
- Added Claude-powered natural-language query support.
- Added RBAC analysis tools.
- Added CI/CD workflows and GoReleaser-based Homebrew publishing.

### Removed
- Removed the image-scanning workflow that did not apply to the CLI binary release flow.

[Unreleased]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.21...HEAD
[v0.8.21]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.20...v0.8.21
[v0.8.20]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.19...v0.8.20
[v0.8.19]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.18...v0.8.19
[v0.8.18]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.17...v0.8.18
[v0.8.17]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.16...v0.8.17
[v0.8.16]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.15...v0.8.16
[v0.8.15]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.14...v0.8.15
[v0.8.14]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.13...v0.8.14
[v0.8.13]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.12...v0.8.13
[v0.8.12]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.11...v0.8.12
[v0.8.11]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.10...v0.8.11
[v0.8.10]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.9...v0.8.10
[v0.8.9]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.8...v0.8.9
[v0.8.8]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.7...v0.8.8
[v0.8.7]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.6...v0.8.7
[v0.8.6]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.5...v0.8.6
[v0.8.5]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.4...v0.8.5
[v0.8.4]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.3...v0.8.4
[v0.8.3]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.2...v0.8.3
[v0.8.2]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.1...v0.8.2
[v0.8.1]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.8.0...v0.8.1
[v0.8.0]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.7.1...v0.8.0
[v0.7.1]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.7.0...v0.7.1
[v0.7.0]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.4.3...v0.5.0
[v0.4.3]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.4.2...v0.4.3
[v0.4.2]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.4.1...v0.4.2
[v0.4.1]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.4.0...v0.4.1
[v0.4.0]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.3.2...v0.4.0
[v0.3.2]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.3.1...v0.3.2
[v0.3.1]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.3.0...v0.3.1
[v0.3.0]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/kubestellar/kubestellar-mcp/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/kubestellar/kubestellar-mcp/releases/tag/v0.1.0
