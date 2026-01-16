# Helm Uninstall

Uninstall a Helm release from clusters.

## Usage

Remove a Helm release from one or more clusters. Automatically finds clusters where the release exists.

## Examples

- "Uninstall my-app from all clusters"
- "Helm uninstall nginx from production cluster"
- "Remove the redis release from staging namespace"

## What it does

1. Finds clusters where the release exists (or uses specified clusters)
2. Runs `helm uninstall` on each cluster
3. Reports success/failure per cluster

## MCP Tools Used

- `helm_uninstall` - Uninstall a Helm release

## Implementation

Use the `helm_uninstall` tool with:
- `release_name`: Name of the release to uninstall (required)
- `namespace`: Namespace of the release (default: default)
- `dry_run`: Preview without applying
- `clusters`: Target clusters (auto-detected if not specified)

## Examples of Tool Calls

```json
{
  "release_name": "my-nginx",
  "namespace": "web",
  "dry_run": true
}
```
