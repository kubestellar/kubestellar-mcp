// Package handlers is the dependency-injection seam between the MCP protocol
// server (pkg/mcp/server) and the domain tool handlers it dispatches to.
//
// It is a leaf package: it must never import pkg/mcp/server or any domain
// sub-package, so that domain packages (rbac, policy, workloads, drift, ...)
// can depend on it without creating an import cycle back to the server.
// See kubestellar-mcp#1002.
package handlers

import (
	"context"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubestellar/kubestellar-mcp/pkg/cluster"
	"github.com/kubestellar/kubestellar-mcp/pkg/gitops"
)

// Discoverer enumerates kubeconfig contexts and probes their health.
type Discoverer interface {
	DiscoverClusters(source string) ([]cluster.ClusterInfo, error)
	CheckHealthByContext(contextName string) (*cluster.HealthInfo, error)
}

// ManifestReader reads Kubernetes manifests from a git source.
type ManifestReader interface {
	ReadFromGit(ctx context.Context, source gitops.ManifestSource) ([]gitops.Manifest, error)
	Cleanup()
}

// DriftDetector compares desired manifests against live cluster state.
type DriftDetector interface {
	IsManifestClusterScoped(manifest gitops.Manifest) bool
	DetectDrift(ctx context.Context, manifests []gitops.Manifest, clusterName string) ([]gitops.DriftResult, error)
}

// Deps carries the injectable dependencies a tool handler may consume. Every
// factory is optional: a nil factory makes the corresponding Get*/New* method
// fall back to a real client built from Kubeconfig (or the default kubeconfig
// loading rules when Kubeconfig is empty). Tests inject fakes by setting the
// factory fields.
type Deps struct {
	Kubeconfig            string
	Discoverer            Discoverer
	ClientFactory         func(clusterName string) (kubernetes.Interface, error)
	DynamicClientFactory  func(clusterName string) (dynamic.Interface, error)
	RESTConfigFactory     func(clusterName string) (*rest.Config, error)
	ManifestReaderFactory func() ManifestReader
	DriftDetectorFactory  func(config *rest.Config) (DriftDetector, error)
}

// clientConfig builds a client config for clusterName using Kubeconfig (or
// the default loading rules) and, when clusterName is non-empty, overrides
// the current context with it.
func (d *Deps) clientConfig(clusterName string) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if d.Kubeconfig != "" {
		loadingRules.ExplicitPath = d.Kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if clusterName != "" {
		configOverrides.CurrentContext = clusterName
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
}

// GetClientForCluster returns a typed clientset for clusterName.
func (d *Deps) GetClientForCluster(clusterName string) (kubernetes.Interface, error) {
	if d.ClientFactory != nil {
		return d.ClientFactory(clusterName)
	}
	config, err := d.clientConfig(clusterName).ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

// GetDynamicClientForCluster returns a dynamic client for clusterName.
func (d *Deps) GetDynamicClientForCluster(clusterName string) (dynamic.Interface, error) {
	if d.DynamicClientFactory != nil {
		return d.DynamicClientFactory(clusterName)
	}
	config, err := d.clientConfig(clusterName).ClientConfig()
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(config)
}

// GetRESTConfigForCluster returns a REST config for clusterName.
func (d *Deps) GetRESTConfigForCluster(clusterName string) (*rest.Config, error) {
	if d.RESTConfigFactory != nil {
		return d.RESTConfigFactory(clusterName)
	}
	return d.clientConfig(clusterName).ClientConfig()
}

// NewManifestReader returns a manifest reader, defaulting to the real
// gitops implementation.
func (d *Deps) NewManifestReader() ManifestReader {
	if d.ManifestReaderFactory != nil {
		return d.ManifestReaderFactory()
	}
	return gitops.NewManifestReader()
}

// NewDriftDetector returns a drift detector for config, defaulting to the
// real gitops implementation.
func (d *Deps) NewDriftDetector(config *rest.Config) (DriftDetector, error) {
	if d.DriftDetectorFactory != nil {
		return d.DriftDetectorFactory(config)
	}
	return gitops.NewDriftDetector(config)
}
