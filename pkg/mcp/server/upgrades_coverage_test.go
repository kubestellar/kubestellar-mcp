package server

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// upgradesScheme extends dynamicScheme with OpenShift GVKs needed for upgrade tests.
var upgradesScheme *runtime.Scheme

func init() {
	upgradesScheme = runtime.NewScheme()
	_ = corev1.AddToScheme(upgradesScheme)

	// ClusterVersion
	cvGVK := schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersionList"}
	upgradesScheme.AddKnownTypeWithName(cvGVK, &unstructured.UnstructuredList{})
	cvItemGVK := schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion"}
	upgradesScheme.AddKnownTypeWithName(cvItemGVK, &unstructured.Unstructured{})

	// ClusterOperator
	coGVK := schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ClusterOperatorList"}
	upgradesScheme.AddKnownTypeWithName(coGVK, &unstructured.UnstructuredList{})
	coItemGVK := schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ClusterOperator"}
	upgradesScheme.AddKnownTypeWithName(coItemGVK, &unstructured.Unstructured{})

	// Subscription (OLM)
	subGVK := schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "SubscriptionList"}
	upgradesScheme.AddKnownTypeWithName(subGVK, &unstructured.UnstructuredList{})
	subItemGVK := schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "Subscription"}
	upgradesScheme.AddKnownTypeWithName(subItemGVK, &unstructured.Unstructured{})

	// MachineConfigPool (OpenShift)
	mcpGVK := schema.GroupVersionKind{Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfigPoolList"}
	upgradesScheme.AddKnownTypeWithName(mcpGVK, &unstructured.UnstructuredList{})
	mcpItemGVK := schema.GroupVersionKind{Group: "machineconfiguration.openshift.io", Version: "v1", Kind: "MachineConfigPool"}
	upgradesScheme.AddKnownTypeWithName(mcpItemGVK, &unstructured.Unstructured{})
}

func newUpgradeCoverageServer(k8sObjs []runtime.Object, dynObjs []runtime.Object) *Server {
	fakeK8s := k8sfake.NewSimpleClientset(k8sObjs...)
	fakeDyn := dynfake.NewSimpleDynamicClient(upgradesScheme, dynObjs...)

	return &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return fakeDyn, nil
		},
	}
}

// --- toolGetClusterVersionInfo ---

func TestToolGetClusterVersionInfo_ClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("no kubeconfig")
		},
	}
	result, rpcErr := callTool(t, server, "get_cluster_version_info", map[string]interface{}{"cluster": "bad"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error for missing client")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolGetClusterVersionInfo_DynamicClientError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("dynamic client error")
		},
	}
	result, rpcErr := callTool(t, server, "get_cluster_version_info", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error for missing dynamic client")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create dynamic client") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolGetClusterVersionInfo_VanillaKubernetes(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.28.0"},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	server := newUpgradeCoverageServer([]runtime.Object{node}, nil)
	result, rpcErr := callTool(t, server, "get_cluster_version_info", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Kubernetes") {
		t.Fatalf("expected 'Kubernetes' type, got: %s", text)
	}
	if !strings.Contains(text, "node-1") {
		t.Fatalf("expected node-1 in output, got: %s", text)
	}
}

func TestToolGetClusterVersionInfo_OpenShift(t *testing.T) {
	cv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "ClusterVersion",
			"metadata":   map[string]interface{}{"name": "version"},
			"status": map[string]interface{}{
				"desired": map[string]interface{}{"version": "4.14.5"},
				"history": []interface{}{
					map[string]interface{}{
						"state":   "Completed",
						"version": "4.14.5",
					},
				},
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Available",
						"status": "True",
					},
				},
				"availableUpdates": []interface{}{
					map[string]interface{}{"version": "4.14.6"},
				},
			},
		},
	}
	server := newUpgradeCoverageServer(nil, []runtime.Object{cv})
	result, rpcErr := callTool(t, server, "get_cluster_version_info", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "OpenShift") {
		t.Fatalf("expected 'OpenShift' type, got: %s", text)
	}
}

// --- toolCheckOLMOperatorUpgrades ---

func TestToolCheckOLMOperatorUpgrades_ClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("no client")
		},
	}
	result, rpcErr := callTool(t, server, "check_olm_operator_upgrades", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error for missing client")
	}
}

func TestToolCheckOLMOperatorUpgrades_NoOLM(t *testing.T) {
	// Empty dynamic client - subscriptions resource won't exist, but dynfake returns empty list
	server := newUpgradeCoverageServer(nil, nil)
	result, rpcErr := callTool(t, server, "check_olm_operator_upgrades", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	// With empty list, should show "0" subscriptions found
	if !strings.Contains(text, "0") && !strings.Contains(text, "Not installed") {
		t.Fatalf("expected no subscriptions message, got: %s", text)
	}
}

func TestToolCheckOLMOperatorUpgrades_WithSubscriptions(t *testing.T) {
	sub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "Subscription",
			"metadata": map[string]interface{}{
				"name":      "prometheus-operator",
				"namespace": "openshift-operators",
			},
			"spec": map[string]interface{}{
				"channel":             "stable",
				"installPlanApproval": "Automatic",
			},
			"status": map[string]interface{}{
				"currentCSV": "prometheusoperator.0.65.1",
				"state":      "AtLatestKnown",
			},
		},
	}
	server := newUpgradeCoverageServer(nil, []runtime.Object{sub})
	result, rpcErr := callTool(t, server, "check_olm_operator_upgrades", map[string]interface{}{
		"cluster": "test",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "prometheus-operator") {
		t.Fatalf("expected operator name in output, got: %s", text)
	}
	if !strings.Contains(text, "Up to date") {
		t.Fatalf("expected 'Up to date' status, got: %s", text)
	}
}

func TestToolCheckOLMOperatorUpgrades_WithNamespaceFilter(t *testing.T) {
	sub := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "Subscription",
			"metadata": map[string]interface{}{
				"name":      "test-operator",
				"namespace": "my-namespace",
			},
			"spec": map[string]interface{}{
				"channel":             "alpha",
				"installPlanApproval": "Manual",
			},
			"status": map[string]interface{}{
				"currentCSV": "test.v1.0.0",
				"state":      "UpgradePending",
			},
		},
	}
	server := newUpgradeCoverageServer(nil, []runtime.Object{sub})
	result, rpcErr := callTool(t, server, "check_olm_operator_upgrades", map[string]interface{}{
		"cluster":   "test",
		"namespace": "my-namespace",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "test-operator") {
		t.Fatalf("expected operator in output, got: %s", text)
	}
}

// --- toolGetUpgradeStatus ---

func TestToolGetUpgradeStatus_ClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("no client")
		},
	}
	result, rpcErr := callTool(t, server, "get_upgrade_status", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error")
	}
}

func TestToolGetUpgradeStatus_DynamicClientError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("no dynamic")
		},
	}
	result, rpcErr := callTool(t, server, "get_upgrade_status", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error")
	}
}

func TestToolGetUpgradeStatus_VanillaKubernetes(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.29.0"},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	server := newUpgradeCoverageServer([]runtime.Object{node}, nil)
	result, rpcErr := callTool(t, server, "get_upgrade_status", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Kubernetes") {
		t.Fatalf("expected Kubernetes type, got: %s", text)
	}
	if !strings.Contains(text, "worker-1") {
		t.Fatalf("expected node name in output, got: %s", text)
	}
}

func TestToolGetUpgradeStatus_OpenShiftProgressing(t *testing.T) {
	cv := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "ClusterVersion",
			"metadata":   map[string]interface{}{"name": "version"},
			"status": map[string]interface{}{
				"desired": map[string]interface{}{"version": "4.15.0"},
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    "Progressing",
						"status":  "True",
						"message": "Working toward 4.15.0: 50% complete",
					},
					map[string]interface{}{
						"type":   "Available",
						"status": "True",
					},
				},
			},
		},
	}
	co := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "ClusterOperator",
			"metadata":   map[string]interface{}{"name": "kube-apiserver"},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{"type": "Available", "status": "True"},
					map[string]interface{}{"type": "Progressing", "status": "True"},
					map[string]interface{}{"type": "Degraded", "status": "False"},
				},
			},
		},
	}
	server := newUpgradeCoverageServer(nil, []runtime.Object{cv, co})
	result, rpcErr := callTool(t, server, "get_upgrade_status", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "OpenShift") {
		t.Fatalf("expected OpenShift in output, got: %s", text)
	}
	if !strings.Contains(text, "progress") || !strings.Contains(text, "4.15.0") {
		t.Fatalf("expected progress info, got: %s", text)
	}
}

// --- toolGetUpgradePrerequisites additional coverage ---

func TestToolGetUpgradePrerequisites_DynamicClientError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("no dynamic")
		},
	}
	result, rpcErr := callTool(t, server, "get_upgrade_prerequisites", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected error for missing dynamic client")
	}
}

func TestToolGetUpgradePrerequisites_MultipleNodesWithMixedStatus(t *testing.T) {
	nodes := []runtime.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "master-1"},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.28.0"},
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.28.0"},
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		},
	}
	server := newUpgradeCoverageServer(nodes, nil)
	result, rpcErr := callTool(t, server, "get_upgrade_prerequisites", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "worker-1") {
		t.Fatalf("expected node names in output, got: %s", text)
	}
}
