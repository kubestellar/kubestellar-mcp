# Integration tests

This package holds the end-to-end integration suite for kubestellar-mcp's
MCP tool surface, added to close kubestellar-mcp#1063 ("no end-to-end /
integration test suite for MCP tool surface"). Every file in this package
carries the `//go:build integration` build tag, so it is excluded from the
default `go test ./...` / `make test` fast path and from the per-package
coverage ratchet (`.github/go-package-coverage-ratchet.txt`), which only
scans `pkg/`, `cmd/`, and `internal/`.

## What it tests (Option A: controller-runtime envtest)

We use [`sigs.k8s.io/controller-runtime/pkg/envtest`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest)
to start a real `kube-apiserver` + `etcd` pair per test binary, with no
kubelet and no container runtime. That gives every MCP tool handler a real
API server round-trip (discovery, RBAC error mapping, dynamic-GVR
resolution) for the price of a local process, and it runs unmodified on
GitHub-hosted runners — no Docker-in-Docker, no `kind` cluster, no cluster
secrets.

Covered packages:

- `pkg/deploy/mcp/kubectl` — `kubectl_apply.go`
  (`kubectl_test.go`): applies a ConfigMap manifest via
  `kubectl.HandleKubectlApply`, confirms it round-trips (create then a
  second apply reports `updated`), then deletes it via
  `kubectl.HandleDeleteResource` and confirms envtest reports `not-found`
  on a second delete.
- `pkg/mcp/server/workloads` — `get_pods` (`workloads_test.go`): creates a
  Pod object directly against the envtest API server, then drives the
  `get_pods` tool handler (via `workloads.Register` +
  `handlers.Registry.Find`, exactly as the real MCP protocol server
  dispatches it) and asserts the pod shows up in the tool's output.
- `pkg/mcp/server/rbac` — `get_roles`, `get_cluster_roles`,
  `get_role_bindings`, `get_cluster_role_bindings`, `describe_role`
  (`rbac_test.go`): creates a Role, ClusterRole, RoleBinding, and
  ClusterRoleBinding directly against the envtest API server, then drives
  each tool (via `rbac.Register` + `handlers.Registry.Find`) and asserts on
  the returned output.
- `pkg/deploy/mcp/labels` — `add_labels`/`remove_labels`
  (`labels_test.go`): seeds a ConfigMap directly against the envtest API
  server, then drives `labels.HandleAddLabels`/`labels.HandleRemoveLabels`
  (wired the same way as `kubectl_test.go`'s `newKubectlDeps`) and asserts
  the label round-trips on both the tool's own result and a direct client
  `Get`, plus a not-found case for a resource that was never created.
- `pkg/mcp/server/diagnostics` — `find_pod_issues`,
  `find_deployment_issues`, `check_resource_limits`,
  `check_security_issues`, `analyze_namespace`, `get_warning_events`
  (`diagnostics_test.go`): seeds an unhealthy Pod (CrashLoopBackOff,
  privileged, no resource limits), a healthy control Pod, a partially
  available Deployment, a Service, and a Warning Event — including
  `/status` subresource writes, since envtest runs no kubelet or
  controller-manager — then drives each tool through
  `diagnostics.Register` + `handlers.Registry.Find` and asserts on the
  returned output. This covers the real `type=Warning` field selector and
  real status round-trips that the package's unit tests fake.
- `pkg/mcp/server/cluster` — `list_clusters`, `get_cluster_health`
  (`cluster_test.go`): writes a real kubeconfig for the envtest control
  plane and wires the real `pkg/cluster.Discoverer` behind
  `handlers.Deps`, so context enumeration, TLS client construction,
  `Discovery().ServerVersion()`, and `Nodes().List()` all run live (envtest
  has no kubelet, so health reports `0/0` nodes ready).
- `pkg/mcp/server/drift` — `detect_drift` (`drift_test.go`): stubs only the
  git half (the `ManifestReader`) and lets the real
  `gitops.DriftDetector` run against envtest, so RESTMapper discovery,
  dynamic-client GETs, and the missing/modified/synced classification are
  exercised end-to-end against seeded ConfigMaps.
- `pkg/mcp/server/policy` — `check_gatekeeper`,
  `get_ownership_policy_status`, `list_ownership_violations`,
  `install_ownership_policy`, `set_ownership_policy_mode`,
  `uninstall_ownership_policy` (`policy_test.go`): installs open-schema
  CRDs for `ConstraintTemplate` and the `K8sRequiredLabels` constraint
  (Gatekeeper itself has no controller or webhook in envtest, but the tools
  only do dynamic Get/List/Create/Update/Delete on those GVRs), then walks
  the whole install → inspect → change mode → list violations → uninstall
  lifecycle, including the "already exists" update branch and the
  not-found branches before install and after uninstall.
- `pkg/deploy/mcp/deploy` — `scale_app`, `patch_app`,
  `list_cluster_capabilities` (`deploy_test.go`): seeds a Deployment
  directly through the typed clientset, then drives `deploy.HandleScaleApp`
  (verifying the new replica count on the live object),
  `deploy.HandlePatchApp` (verifying a strategic-merge label patch on the
  live object), and `deploy.HandleListClusterCapabilities` (the real
  `Nodes().List` path — envtest has no kubelet, so it reports zero nodes),
  plus an app-not-found case. `deploy_app`'s git-manifest-syncer path is
  left to the package's own unit tests. Wired the same way as
  `kubectl_test.go`/`labels_test.go`.
- `pkg/deploy/mcp/app` — `get_app_instances`, `get_app_status`
  (`app_test.go`): seeds a Deployment with a hand-written `/status`
  reporting full availability (envtest runs no controller-manager), then
  drives `app.GetAppInstances` and `app.GetAppStatus` through the exported
  handlers the `app_adapter.go` wires into the deploy server, asserting the
  instance is found and the app aggregates as `healthy`, plus a not-found
  case. `get_app_logs` is out of scope — pod-log streaming needs a real
  kubelet.
- `pkg/mcp/tools/upgrades` — `detect_cluster_type`,
  `get_cluster_version_info`, `get_upgrade_status`,
  `get_upgrade_prerequisites` (`upgrades_test.go`): wires the real typed +
  dynamic clients behind `upgrades.ClusterAccess`, so each tool's OpenShift
  `ClusterVersion` probe (a real dynamic Get that returns not-found here)
  falls through to its vanilla-Kubernetes branch —
  `Discovery().ServerVersion`, `Nodes().List`, and `Pods().List` all run
  live against the apiserver. The OpenShift/OLM/Helm-only tools
  (`check_olm_operator_upgrades`, `check_helm_release_upgrades`,
  `trigger_openshift_upgrade`) need CRDs / Helm-release secrets and stay
  with the package's unit tests.

Still uncovered (tracked in kubestellar-mcp#1070): `pkg/deploy/mcp/kustomize`
(shells out to the `kustomize`/`kubectl` binaries with a real kubeconfig
`--context`, so it fits a kind-based Option B suite better than the pure
apiserver envtest harness), `pkg/deploy/mcp/gitops`
(`sync_from_git`/`reconcile`/`preview_changes`, which read manifests from a
git source), `pkg/deploy/mcp/helm` (see below), the OpenShift/OLM/Helm-only
`upgrades` tools noted above, plus the
`can_i`/`analyze_subject_permissions`/`audit_kubeconfig`/`find_resource_owners`
tools noted in `rbac_test.go`.

### Why `pkg/deploy/mcp/helm` is out of scope for envtest

`envtest` only runs `kube-apiserver` + `etcd`; there is no kubelet, no
container runtime, and no `helm` binary in the CI image by default. A real
`helm install` round-trip also depends on chart repository access (network)
or a bundled local chart plus the Helm SDK's storage driver, which layers
significant harness complexity on top of the base envtest setup for a tool
that is a thin wrapper around the `helm` CLI/SDK rather than direct
apiserver calls. Per the issue's own recommendation, this is a documented
gap rather than a forced-fit envtest test: revisit if/when a kind-based
(Option B) suite is introduced, which does have a real kubelet and can
install a chart end-to-end.

## Running locally

Prerequisites: Go (matching `go.mod`), and `setup-envtest` to fetch the
`kube-apiserver`/`etcd`/`kubectl` binaries envtest needs (no live cluster or
kubeconfig required):

```sh
go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
export KUBEBUILDER_ASSETS="$(setup-envtest use -p path 1.30.x)"
make test-integration
```

`make test-integration` runs `go test -tags integration ./test/integration/...`.

## CI

`.github/workflows/integration-test.yml` runs this suite on pull requests
that touch `pkg/**`, `internal/**`, `test/integration/**`, `Makefile`,
`go.mod`, or `go.sum`. It installs `setup-envtest`, downloads the
`kube-apiserver`/`etcd` binaries into `KUBEBUILDER_ASSETS`, and runs
`make test-integration`.
