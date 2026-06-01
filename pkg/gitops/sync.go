package gitops

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// SyncAction represents what action was taken for a resource
type SyncAction string

const (
	SyncActionCreated   SyncAction = "created"
	SyncActionUpdated   SyncAction = "updated"
	SyncActionUnchanged SyncAction = "unchanged"
	SyncActionSkipped   SyncAction = "skipped"
	SyncActionFailed    SyncAction = "failed"
)

// SyncResult represents the result of syncing a single resource
type SyncResult struct {
	Cluster   string     `json:"cluster"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	Namespace string     `json:"namespace,omitempty"`
	Action    SyncAction `json:"action"`
	Message   string     `json:"message,omitempty"`
}

// SyncSummary provides an overview of sync operation
type SyncSummary struct {
	Cluster   string       `json:"cluster"`
	Created   int          `json:"created"`
	Updated   int          `json:"updated"`
	Unchanged int          `json:"unchanged"`
	Failed    int          `json:"failed"`
	Skipped   int          `json:"skipped"`
	Results   []SyncResult `json:"results"`
}

// Syncer synchronizes manifests to clusters
type Syncer struct {
	dynClient  dynamic.Interface
	restMapper meta.RESTMapper
}

// NewSyncer creates a new syncer
func NewSyncer(config *rest.Config) (*Syncer, error) {
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Syncer{
		dynClient:  dynClient,
		restMapper: newRESTMapper(config),
	}, nil
}

// SyncOptions controls sync behavior
type SyncOptions struct {
	DryRun    bool     // Preview changes without applying
	Namespace string   // Override namespace for all resources
	Include   []string // Only sync these kinds
	Exclude   []string // Don't sync these kinds
}

// Sync applies manifests to a cluster
func (s *Syncer) Sync(ctx context.Context, manifests []Manifest, clusterName string, opts SyncOptions) (*SyncSummary, error) {
	summary := &SyncSummary{
		Cluster: clusterName,
		Results: []SyncResult{},
	}

	for _, manifest := range manifests {
		// Check if kind should be included/excluded
		if !s.shouldSync(manifest.Kind, opts) {
			summary.Skipped++
			summary.Results = append(summary.Results, SyncResult{
				Cluster:   clusterName,
				Kind:      manifest.Kind,
				Name:      manifest.Metadata.Name,
				Namespace: manifest.GetNamespace(),
				Action:    SyncActionSkipped,
				Message:   "Kind excluded from sync",
			})
			continue
		}

		mapping, err := resolveManifestResource(manifest, s.restMapper)
		if err != nil {
			summary.Failed++
			summary.Results = append(summary.Results, SyncResult{
				Cluster:   clusterName,
				Kind:      manifest.Kind,
				Name:      manifest.Metadata.Name,
				Namespace: manifest.Metadata.Namespace,
				Action:    SyncActionFailed,
				Message:   fmt.Sprintf("failed to resolve resource mapping: %v", err),
			})
			continue
		}

		var namespace string
		if !mapping.ClusterScoped {
			namespace = manifest.GetNamespace()
			if opts.Namespace != "" {
				namespace = opts.Namespace
			}
		}

		result, err := s.syncResource(ctx, manifest, mapping, namespace, opts.DryRun)
		if err != nil {
			summary.Failed++
			summary.Results = append(summary.Results, SyncResult{
				Cluster:   clusterName,
				Kind:      manifest.Kind,
				Name:      manifest.Metadata.Name,
				Namespace: namespace,
				Action:    SyncActionFailed,
				Message:   err.Error(),
			})
			continue
		}

		result.Cluster = clusterName
		summary.Results = append(summary.Results, *result)

		switch result.Action {
		case SyncActionCreated:
			summary.Created++
		case SyncActionUpdated:
			summary.Updated++
		case SyncActionUnchanged:
			summary.Unchanged++
		}
	}

	return summary, nil
}

// syncResource syncs a single resource
func (s *Syncer) syncResource(ctx context.Context, manifest Manifest, mapping resourceMapping, namespace string, dryRun bool) (*SyncResult, error) {
	// Create unstructured object from manifest
	obj := &unstructured.Unstructured{Object: manifest.Raw}

	if !mapping.ClusterScoped && namespace != "" {
		obj.SetNamespace(namespace)
	}

	result := &SyncResult{
		Kind:      manifest.Kind,
		Name:      manifest.Metadata.Name,
		Namespace: namespace,
	}

	var (
		existing *unstructured.Unstructured
		err      error
	)
	if mapping.ClusterScoped {
		existing, err = s.dynClient.Resource(mapping.GVR).Get(ctx, manifest.Metadata.Name, metav1.GetOptions{})
	} else {
		existing, err = s.dynClient.Resource(mapping.GVR).Namespace(namespace).Get(ctx, manifest.Metadata.Name, metav1.GetOptions{})
	}

	if err != nil {
		// Only proceed with create if resource truly doesn't exist
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get resource: %w", err)
		}

		// Resource doesn't exist - create it
		if dryRun {
			result.Action = SyncActionCreated
			result.Message = "Would create (dry-run)"
			return result, nil
		}

		var created *unstructured.Unstructured
		if mapping.ClusterScoped {
			created, err = s.dynClient.Resource(mapping.GVR).Create(ctx, obj, metav1.CreateOptions{})
		} else {
			created, err = s.dynClient.Resource(mapping.GVR).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
		}

		if err != nil {
			return nil, fmt.Errorf("failed to create: %w", err)
		}

		result.Action = SyncActionCreated
		result.Message = fmt.Sprintf("Created %s", created.GetUID())
		return result, nil
	}

	// Resource exists - update it using server-side apply
	if dryRun {
		result.Action = SyncActionUpdated
		result.Message = "Would update (dry-run)"
		return result, nil
	}

	// Use server-side apply for updates
	data, err := json.Marshal(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal: %w", err)
	}

	var updated *unstructured.Unstructured
	if mapping.ClusterScoped {
		updated, err = s.dynClient.Resource(mapping.GVR).Patch(ctx, manifest.Metadata.Name,
			types.ApplyPatchType, data, metav1.PatchOptions{
				FieldManager: "kubestellar-deploy",
				Force:        boolPtr(true),
			})
	} else {
		updated, err = s.dynClient.Resource(mapping.GVR).Namespace(namespace).Patch(ctx, manifest.Metadata.Name,
			types.ApplyPatchType, data, metav1.PatchOptions{
				FieldManager: "kubestellar-deploy",
				Force:        boolPtr(true),
			})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to update: %w", err)
	}

	// Check if anything actually changed
	if existing.GetResourceVersion() == updated.GetResourceVersion() {
		result.Action = SyncActionUnchanged
		result.Message = "No changes"
	} else {
		result.Action = SyncActionUpdated
		result.Message = fmt.Sprintf("Updated (rv: %s -> %s)", existing.GetResourceVersion(), updated.GetResourceVersion())
	}

	return result, nil
}

// shouldSync checks if a kind should be synced
func (s *Syncer) shouldSync(kind string, opts SyncOptions) bool {
	// Check excludes first
	for _, exclude := range opts.Exclude {
		if exclude == kind {
			return false
		}
	}

	// If includes are specified, kind must be in the list
	if len(opts.Include) > 0 {
		for _, include := range opts.Include {
			if include == kind {
				return true
			}
		}
		return false
	}

	return true
}

// getGVR returns the GroupVersionResource for a manifest.
func (s *Syncer) getGVR(manifest Manifest) (schema.GroupVersionResource, error) {
	mapping, err := resolveManifestResource(manifest, s.restMapper)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	return mapping.GVR, nil
}

func boolPtr(b bool) *bool {
	return &b
}
