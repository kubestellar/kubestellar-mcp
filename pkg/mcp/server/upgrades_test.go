package server

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func newUpgradeTestServer(k8sObjs []runtime.Object, dynObjs []runtime.Object) *Server {
	fakeK8s := k8sfake.NewSimpleClientset(k8sObjs...)
	fakeDyn := dynfake.NewSimpleDynamicClient(dynamicScheme, dynObjs...)

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

// --- toolDetectClusterType ---

func TestToolDetectClusterType_ClientFactoryError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("no kubeconfig")
		},
	}
	result, rpcErr := callTool(t, server, "detect_cluster_type", map[string]interface{}{"cluster": "bad"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error for missing client")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("unexpected error message: %s", result.Content[0].Text)
	}
}

func TestToolDetectClusterType_DynamicClientError(t *testing.T) {
	fakeK8s := k8sfake.NewSimpleClientset()
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return fakeK8s, nil
		},
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
			return nil, errors.New("dynamic client broken")
		},
	}
	result, rpcErr := callTool(t, server, "detect_cluster_type", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error for missing dynamic client")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create dynamic client") {
		t.Fatalf("unexpected error message: %s", result.Content[0].Text)
	}
}

func TestToolDetectClusterType_NoNodes(t *testing.T) {
	server := newUpgradeTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "detect_cluster_type", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Cluster Type Detection") {
		t.Fatalf("expected cluster type detection header, got: %s", text)
	}
	if !strings.Contains(text, "unknown") && !strings.Contains(text, "No nodes found") {
		t.Fatalf("expected unknown cluster type or no nodes message, got: %s", text)
	}
}

// --- toolCheckHelmReleaseUpgrades ---

func TestToolCheckHelmReleaseUpgrades_ClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("connection refused")
		},
	}
	result, rpcErr := callTool(t, server, "check_helm_release_upgrades", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error")
	}
	if !strings.Contains(result.Content[0].Text, "Failed to create client") {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestToolCheckHelmReleaseUpgrades_NoReleases(t *testing.T) {
	server := newUpgradeTestServer(nil, nil)
	result, rpcErr := callTool(t, server, "check_helm_release_upgrades", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Helm Releases Found:** 0") {
		t.Fatalf("expected 0 releases, got: %s", text)
	}
}

func TestToolCheckHelmReleaseUpgrades_WithRelease(t *testing.T) {
	helmData := makeHelmReleaseSecret("my-app", "default", "my-chart", "1.2.3", "2.0.0", "deployed", 3)
	server := newUpgradeTestServer([]runtime.Object{helmData}, nil)
	result, rpcErr := callTool(t, server, "check_helm_release_upgrades", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "my-app") {
		t.Fatalf("expected release name in output, got: %s", text)
	}
	if !strings.Contains(text, "my-chart") {
		t.Fatalf("expected chart name in output, got: %s", text)
	}
}

func TestToolCheckHelmReleaseUpgrades_WithNamespaceFilter(t *testing.T) {
	secret1 := makeHelmReleaseSecret("app1", "ns1", "chart1", "1.0.0", "1.0.0", "deployed", 1)
	secret2 := makeHelmReleaseSecret("app2", "ns2", "chart2", "2.0.0", "2.0.0", "deployed", 1)
	server := newUpgradeTestServer([]runtime.Object{secret1, secret2}, nil)

	result, rpcErr := callTool(t, server, "check_helm_release_upgrades", map[string]interface{}{
		"cluster":   "test",
		"namespace": "ns1",
	})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "Helm Releases") {
		t.Fatalf("expected Helm Releases header, got: %s", text)
	}
}

// --- toolGetUpgradePrerequisites ---

func TestToolGetUpgradePrerequisites_ClientError(t *testing.T) {
	server := &Server{
		discoverer: stubDiscoverer{},
		clientFactory: func(clusterName string) (kubernetes.Interface, error) {
			return nil, errors.New("no access")
		},
	}
	result, rpcErr := callTool(t, server, "get_upgrade_prerequisites", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if !result.IsError {
		t.Fatalf("expected tool error")
	}
}

func TestToolGetUpgradePrerequisites_HealthyCluster(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	server := newUpgradeTestServer([]runtime.Object{node}, nil)
	result, rpcErr := callTool(t, server, "get_upgrade_prerequisites", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "All nodes ready") {
		t.Fatalf("expected all nodes ready, got: %s", text)
	}
}

func TestToolGetUpgradePrerequisites_NotReadyNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-node"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
		},
	}
	server := newUpgradeTestServer([]runtime.Object{node}, nil)
	result, rpcErr := callTool(t, server, "get_upgrade_prerequisites", map[string]interface{}{"cluster": "test"})
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content[0].Text)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "not ready") || !strings.Contains(text, "bad-node") {
		t.Fatalf("expected not-ready node message, got: %s", text)
	}
}

// --- parseHelmSecret ---

func TestParseHelmSecret_WrongType(t *testing.T) {
	s := &Server{}
	secret := &corev1.Secret{
		Type: "Opaque",
		Data: map[string][]byte{"release": []byte("something")},
	}
	result := s.parseHelmSecret(secret)
	if result != nil {
		t.Fatalf("expected nil for non-helm secret, got: %+v", result)
	}
}

func TestParseHelmSecret_MissingReleaseKey(t *testing.T) {
	s := &Server{}
	secret := &corev1.Secret{
		Type: "helm.sh/release.v1",
		Data: map[string][]byte{"other": []byte("data")},
	}
	result := s.parseHelmSecret(secret)
	if result != nil {
		t.Fatalf("expected nil for missing release key, got: %+v", result)
	}
}

func TestParseHelmSecret_InvalidGzip(t *testing.T) {
	s := &Server{}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "sh.helm.release.v1.test.v1", Namespace: "default"},
		Type:       "helm.sh/release.v1",
		Data:       map[string][]byte{"release": []byte("not-valid-gzip-or-base64")},
	}
	result := s.parseHelmSecret(secret)
	if result != nil {
		t.Fatalf("expected nil for invalid gzip data, got: %+v", result)
	}
}

func TestParseHelmSecret_ValidRelease(t *testing.T) {
	s := &Server{}
	secret := makeHelmReleaseSecret("test-release", "kube-system", "test-chart", "3.0.0", "1.5.0", "deployed", 2)
	result := s.parseHelmSecret(secret)
	if result == nil {
		t.Fatal("expected non-nil result for valid helm secret")
	}
	if result.Name != "test-release" {
		t.Fatalf("expected name=test-release, got: %s", result.Name)
	}
	if result.Namespace != "kube-system" {
		t.Fatalf("expected namespace=kube-system, got: %s", result.Namespace)
	}
	if result.Chart != "test-chart" {
		t.Fatalf("expected chart=test-chart, got: %s", result.Chart)
	}
	if result.Version != "3.0.0" {
		t.Fatalf("expected version=3.0.0, got: %s", result.Version)
	}
	if result.AppVer != "1.5.0" {
		t.Fatalf("expected appVersion=1.5.0, got: %s", result.AppVer)
	}
	if result.Status != "deployed" {
		t.Fatalf("expected status=deployed, got: %s", result.Status)
	}
	if result.Revision != 2 {
		t.Fatalf("expected revision=2, got: %d", result.Revision)
	}
}

// --- Cluster type constants ---

func TestClusterTypeConstants(t *testing.T) {
	types := []string{
		ClusterTypeOpenShift, ClusterTypeEKS, ClusterTypeGKE,
		ClusterTypeAKS, ClusterTypeKubeadm, ClusterTypeK3s,
		ClusterTypeKind, ClusterTypeMinikube, ClusterTypeUnknown,
	}
	seen := make(map[string]bool)
	for _, ct := range types {
		if ct == "" {
			t.Fatal("cluster type constant is empty")
		}
		if seen[ct] {
			t.Fatalf("duplicate cluster type constant: %s", ct)
		}
		seen[ct] = true
	}
}

// --- Helper ---

func makeHelmReleaseSecret(name, namespace, chart, version, appVersion, status string, revision int) *corev1.Secret {
	releaseObj := map[string]interface{}{
		"name":    name,
		"version": float64(revision),
		"info": map[string]interface{}{
			"status": status,
		},
		"chart": map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":       chart,
				"version":    version,
				"appVersion": appVersion,
			},
		},
	}

	jsonData, _ := json.Marshal(releaseObj)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write(jsonData)
	_ = gz.Close()

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1." + name + ".v1",
			Namespace: namespace,
			Labels:    map[string]string{"owner": "helm"},
		},
		Type: "helm.sh/release.v1",
		Data: map[string][]byte{
			"release": []byte(encoded),
		},
	}
}
