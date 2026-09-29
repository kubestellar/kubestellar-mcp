package workloads

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestFormattingHelpers(t *testing.T) {
	ports := formatPorts([]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}, {Port: 443, NodePort: 30443, Protocol: corev1.ProtocolTCP}})
	if ports != "80/TCP,443:30443/TCP" {
		t.Fatalf("formatPorts() = %q", ports)
	}
}
