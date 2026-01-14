# kubectl-claude

AI-powered kubectl plugin for multi-cluster Kubernetes management.

## Overview

`kubectl-claude` is a kubectl plugin that helps you manage clusters and deployments across multiple Kubernetes clusters, powered by Claude AI.

### Features

- **Multi-cluster discovery** - Discover clusters from kubeconfig and KubeStellar
- **Deployment management** - Deploy, rollout, and scale across clusters
- **AI-powered assistance** - Natural language queries and intelligent recommendations
- **MCP server mode** - Direct integration with Claude Code and AI IDEs

## Installation

### From Source

```bash
git clone https://github.com/kubestellar/kubectl-claude.git
cd kubectl-claude
make build
make install
```

### Using Go Install

```bash
go install github.com/kubestellar/kubectl-claude/cmd/kubectl-claude@latest
```

## Usage

### List Clusters

```bash
# List all clusters from kubeconfig
kubectl claude clusters list

# List clusters from specific source
kubectl claude clusters list --source=kubeconfig
kubectl claude clusters list --source=kubestellar
```

### Check Cluster Health

```bash
# Check health of all clusters
kubectl claude clusters health --all-clusters

# Check health of specific cluster
kubectl claude clusters health prod-east
```

### Deploy to Multiple Clusters (Coming Soon)

```bash
kubectl claude deploy nginx:1.25 --clusters=prod-east,prod-west
```

### Natural Language Queries (Coming Soon)

```bash
kubectl claude "show me all failing pods across all clusters"
kubectl claude "why is my nginx deployment not healthy?"
```

## Development

### Prerequisites

- Go 1.22+
- kubectl configured with cluster access

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Running

```bash
./bin/kubectl-claude clusters list
```

## Architecture

kubectl-claude operates in two modes:

1. **kubectl plugin mode** - `kubectl claude ...` for terminal users
2. **MCP server mode** - `kubectl-claude --mcp-server` for Claude Code integration

## Contributing

Contributions are welcome! Please read our [contributing guidelines](CONTRIBUTING.md).

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
