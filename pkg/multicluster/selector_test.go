package multicluster

import "testing"

func TestClusterMeetsRequirements(t *testing.T) {
	tests := []struct {
		name string
		cap  ClusterCapabilities
		req  WorkloadRequirements
		want bool
	}{
		{
			name: "matches cpu memory gpu and labels",
			cap: ClusterCapabilities{
				AllocatableCPU:    "8",
				AllocatableMemory: "16Gi",
				GPUs:              []GPUInfo{{Type: "nvidia.com/gpu", Quantity: 2}},
				Labels:            map[string]string{"topology.kubernetes.io/region": "us-east-1"},
			},
			req: WorkloadRequirements{
				MinCPU:     "4",
				MinMemory:  "8Gi",
				GPUType:    "nvidia.com/gpu",
				MinGPU:     2,
				NodeLabels: map[string]string{"topology.kubernetes.io/region": "us-east-1"},
			},
			want: true,
		},
		{
			name: "rejects insufficient total gpu",
			cap: ClusterCapabilities{
				AllocatableCPU:    "8",
				AllocatableMemory: "16Gi",
				GPUs:              []GPUInfo{{Type: "nvidia.com/gpu", Quantity: 1}},
			},
			req:  WorkloadRequirements{MinGPU: 2},
			want: false,
		},
		{
			name: "rejects missing gpu type",
			cap:  ClusterCapabilities{GPUs: []GPUInfo{{Type: "amd.com/gpu", Quantity: 4}}},
			req:  WorkloadRequirements{GPUType: "nvidia.com/gpu", MinGPU: 1},
			want: false,
		},
		{
			name: "rejects insufficient memory",
			cap:  ClusterCapabilities{AllocatableCPU: "8", AllocatableMemory: "4Gi"},
			req:  WorkloadRequirements{MinMemory: "8Gi"},
			want: false,
		},
		{
			name: "rejects label mismatch",
			cap:  ClusterCapabilities{AllocatableCPU: "8", AllocatableMemory: "16Gi", Labels: map[string]string{"kubernetes.io/arch": "arm64"}},
			req:  WorkloadRequirements{NodeLabels: map[string]string{"kubernetes.io/arch": "amd64"}},
			want: false,
		},
		{
			name: "ignores invalid requested quantity",
			cap:  ClusterCapabilities{AllocatableCPU: "1", AllocatableMemory: "1Gi"},
			req:  WorkloadRequirements{MinCPU: "not-a-quantity", MinMemory: "still-not-valid"},
			want: true,
		},
	}

	s := &Selector{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.clusterMeetsRequirements(tt.cap, tt.req); got != tt.want {
				t.Fatalf("clusterMeetsRequirements() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsGPUResource(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		want     bool
	}{
		{name: "nvidia", resource: "nvidia.com/gpu", want: true},
		{name: "habana", resource: "habana.ai/gaudi", want: true},
		{name: "non gpu", resource: "cpu", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGPUResource(tt.resource); got != tt.want {
				t.Fatalf("isGPUResource(%q) = %v, want %v", tt.resource, got, tt.want)
			}
		})
	}
}

func TestIsClusterLabel(t *testing.T) {
	tests := []struct {
		label string
		want  bool
	}{
		{label: "topology.kubernetes.io/region", want: true},
		{label: "node.kubernetes.io/instance-type", want: true},
		{label: "custom.example.com/team", want: false},
	}

	for _, tt := range tests {
		if got := isClusterLabel(tt.label); got != tt.want {
			t.Fatalf("isClusterLabel(%q) = %v, want %v", tt.label, got, tt.want)
		}
	}
}
