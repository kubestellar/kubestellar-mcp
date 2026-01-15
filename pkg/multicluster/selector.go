package multicluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterCapabilities represents what a cluster can run
type ClusterCapabilities struct {
	Cluster       string            `json:"cluster"`
	NodeCount     int               `json:"nodeCount"`
	ReadyNodes    int               `json:"readyNodes"`
	TotalCPU      string            `json:"totalCpu"`
	TotalMemory   string            `json:"totalMemory"`
	AllocatableCPU    string        `json:"allocatableCpu"`
	AllocatableMemory string        `json:"allocatableMemory"`
	GPUs          []GPUInfo         `json:"gpus,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// GPUInfo represents GPU availability
type GPUInfo struct {
	Type     string `json:"type"`     // nvidia.com/gpu, amd.com/gpu, etc.
	Quantity int64  `json:"quantity"` // Total available
}

// WorkloadRequirements defines what a workload needs
type WorkloadRequirements struct {
	MinCPU      string            `json:"minCpu,omitempty"`
	MinMemory   string            `json:"minMemory,omitempty"`
	GPUType     string            `json:"gpuType,omitempty"`
	MinGPU      int64             `json:"minGpu,omitempty"`
	NodeLabels  map[string]string `json:"nodeLabels,omitempty"`
}

// Selector handles cluster selection based on workload requirements
type Selector struct {
	executor *Executor
}

// NewSelector creates a new cluster selector
func NewSelector(executor *Executor) *Selector {
	return &Selector{
		executor: executor,
	}
}

// GetClusterCapabilities returns capabilities for all clusters
func (s *Selector) GetClusterCapabilities(ctx context.Context) ([]ClusterCapabilities, error) {
	results, err := s.executor.Execute(ctx, "", func(ctx context.Context, client *kubernetes.Clientset, clusterName string) (interface{}, error) {
		return s.GetCapabilitiesForCluster(ctx, client, clusterName)
	})
	if err != nil {
		return nil, err
	}

	var capabilities []ClusterCapabilities
	for _, result := range results {
		if result.Error != "" {
			capabilities = append(capabilities, ClusterCapabilities{
				Cluster: result.Cluster,
			})
			continue
		}
		if cap, ok := result.Result.(*ClusterCapabilities); ok {
			capabilities = append(capabilities, *cap)
		}
	}

	return capabilities, nil
}

// GetCapabilitiesForCluster gets capabilities for a single cluster
func (s *Selector) GetCapabilitiesForCluster(ctx context.Context, client *kubernetes.Clientset, clusterName string) (*ClusterCapabilities, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	cap := &ClusterCapabilities{
		Cluster:   clusterName,
		NodeCount: len(nodes.Items),
		Labels:    make(map[string]string),
	}

	var totalCPU, totalMemory resource.Quantity
	var allocatableCPU, allocatableMemory resource.Quantity
	gpuCounts := make(map[string]int64)

	for _, node := range nodes.Items {
		// Count ready nodes
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				cap.ReadyNodes++
				break
			}
		}

		// Sum resources
		if cpu := node.Status.Capacity.Cpu(); cpu != nil {
			totalCPU.Add(*cpu)
		}
		if mem := node.Status.Capacity.Memory(); mem != nil {
			totalMemory.Add(*mem)
		}
		if cpu := node.Status.Allocatable.Cpu(); cpu != nil {
			allocatableCPU.Add(*cpu)
		}
		if mem := node.Status.Allocatable.Memory(); mem != nil {
			allocatableMemory.Add(*mem)
		}

		// Check for GPUs
		for resourceName, quantity := range node.Status.Allocatable {
			if isGPUResource(string(resourceName)) {
				gpuCounts[string(resourceName)] += quantity.Value()
			}
		}

		// Collect common labels
		for key, value := range node.Labels {
			if isClusterLabel(key) {
				cap.Labels[key] = value
			}
		}
	}

	cap.TotalCPU = totalCPU.String()
	cap.TotalMemory = totalMemory.String()
	cap.AllocatableCPU = allocatableCPU.String()
	cap.AllocatableMemory = allocatableMemory.String()

	for gpuType, count := range gpuCounts {
		cap.GPUs = append(cap.GPUs, GPUInfo{
			Type:     gpuType,
			Quantity: count,
		})
	}

	return cap, nil
}

// FindClustersForWorkload finds clusters that can run the specified workload
func (s *Selector) FindClustersForWorkload(ctx context.Context, req WorkloadRequirements) ([]string, error) {
	capabilities, err := s.GetClusterCapabilities(ctx)
	if err != nil {
		return nil, err
	}

	var matchingClusters []string
	for _, cap := range capabilities {
		if s.clusterMeetsRequirements(cap, req) {
			matchingClusters = append(matchingClusters, cap.Cluster)
		}
	}

	return matchingClusters, nil
}

// clusterMeetsRequirements checks if a cluster meets workload requirements
func (s *Selector) clusterMeetsRequirements(cap ClusterCapabilities, req WorkloadRequirements) bool {
	// Check GPU requirements
	if req.GPUType != "" {
		hasGPU := false
		for _, gpu := range cap.GPUs {
			if gpu.Type == req.GPUType && gpu.Quantity >= req.MinGPU {
				hasGPU = true
				break
			}
		}
		if !hasGPU {
			return false
		}
	} else if req.MinGPU > 0 {
		// Any GPU type
		totalGPU := int64(0)
		for _, gpu := range cap.GPUs {
			totalGPU += gpu.Quantity
		}
		if totalGPU < req.MinGPU {
			return false
		}
	}

	// Check memory requirements
	if req.MinMemory != "" {
		required, err := resource.ParseQuantity(req.MinMemory)
		if err == nil {
			available, err := resource.ParseQuantity(cap.AllocatableMemory)
			if err != nil || available.Cmp(required) < 0 {
				return false
			}
		}
	}

	// Check CPU requirements
	if req.MinCPU != "" {
		required, err := resource.ParseQuantity(req.MinCPU)
		if err == nil {
			available, err := resource.ParseQuantity(cap.AllocatableCPU)
			if err != nil || available.Cmp(required) < 0 {
				return false
			}
		}
	}

	// Check node labels
	for key, value := range req.NodeLabels {
		if cap.Labels[key] != value {
			return false
		}
	}

	return true
}

// isGPUResource checks if a resource name represents a GPU
func isGPUResource(resourceName string) bool {
	gpuResources := []string{
		"nvidia.com/gpu",
		"amd.com/gpu",
		"intel.com/gpu",
		"habana.ai/gaudi",
	}
	for _, gpu := range gpuResources {
		if resourceName == gpu {
			return true
		}
	}
	return false
}

// isClusterLabel checks if a label is relevant for cluster identification
func isClusterLabel(key string) bool {
	clusterLabels := []string{
		"topology.kubernetes.io/region",
		"topology.kubernetes.io/zone",
		"node.kubernetes.io/instance-type",
		"kubernetes.io/arch",
		"kubernetes.io/os",
	}
	for _, label := range clusterLabels {
		if key == label {
			return true
		}
	}
	return false
}
