package gitops

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
)

type resourceMapping struct {
	GVR           schema.GroupVersionResource
	ClusterScoped bool
}

func newRESTMapper(config *rest.Config) meta.RESTMapper {
	dc, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		klog.Warningf("could not create discovery client for RESTMapper: %v; falling back to static mapping", err)
		return nil
	}

	gr, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		klog.Warningf("could not fetch API group resources for RESTMapper: %v; falling back to static mapping", err)
		return nil
	}

	return restmapper.NewDiscoveryRESTMapper(gr)
}

func resolveManifestResource(manifest Manifest, mapper meta.RESTMapper) (resourceMapping, error) {
	gv, err := schema.ParseGroupVersion(manifest.APIVersion)
	if err != nil {
		return resourceMapping{}, fmt.Errorf("parse apiVersion %q: %w", manifest.APIVersion, err)
	}

	if mapper != nil {
		mapping, err := mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: manifest.Kind}, gv.Version)
		if err == nil {
			return resourceMapping{
				GVR:           mapping.Resource,
				ClusterScoped: mapping.Scope != nil && mapping.Scope.Name() == meta.RESTScopeNameRoot,
			}, nil
		}
		klog.V(2).Infof("RESTMapper lookup failed for kind=%s group=%s version=%s: %v; falling back to static mapping",
			manifest.Kind, gv.Group, gv.Version, err)
	}

	return resourceMapping{
		GVR: schema.GroupVersionResource{
			Group:    gv.Group,
			Version:  gv.Version,
			Resource: kindToResource(manifest.Kind),
		},
		ClusterScoped: IsClusterScoped(manifest.Kind),
	}, nil
}
