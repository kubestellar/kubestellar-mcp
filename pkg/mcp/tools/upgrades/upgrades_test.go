package upgrades

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// mockClusterAccess implements ClusterAccess for unit tests.
type mockClusterAccess struct {
	client    kubernetes.Interface
	dynClient dynamic.Interface
	clientErr error
	dynErr    error
}

func (m *mockClusterAccess) GetClientForCluster(_ string) (kubernetes.Interface, error) {
	return m.client, m.clientErr
}

func (m *mockClusterAccess) GetDynamicClientForCluster(_ string) (dynamic.Interface, error) {
	return m.dynClient, m.dynErr
}

// newFakeClientWithVersion sets up a fake kubernetes client with a given server version.
func newFakeClientWithVersion(gitVersion string, objects ...runtime.Object) *kubefake.Clientset {
	cs := kubefake.NewSimpleClientset(objects...)
	fakeDiscovery, ok := cs.Discovery().(*fakediscovery.FakeDiscovery)
	if ok {
		fakeDiscovery.FakedServerVersion = &version.Info{
			GitVersion: gitVersion,
			Platform:   "linux/amd64",
			BuildDate:  "2025-01-01T00:00:00Z",
		}
	}
	return cs
}

// makeHelmSecret creates a test Helm release secret with gzip+base64 encoding.
func makeHelmSecret(name, namespace, chartName, chartVersion, appVersion, status string, revision int) *corev1.Secret {
	releaseObj := map[string]interface{}{
		"name":    name,
		"version": float64(revision),
		"info": map[string]interface{}{
			"status": status,
		},
		"chart": map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":       chartName,
				"version":    chartVersion,
				"appVersion": appVersion,
			},
		},
	}
	data, _ := json.Marshal(releaseObj)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(data)
	_ = gw.Close()

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, revision),
			Namespace: namespace,
			Labels:    map[string]string{"owner": "helm"},
		},
		Type: "helm.sh/release.v1",
		Data: map[string][]byte{
			"release": []byte(encoded),
		},
	}
}

// --- ParseHelmSecret tests ---

func TestParseHelmSecret_ValidRelease(t *testing.T) {
	secret := makeHelmSecret("my-app", "default", "my-chart", "1.2.3", "4.5.6", "deployed", 3)
	release := ParseHelmSecret(secret)

	require.NotNil(t, release)
	assert.Equal(t, "my-app", release.Name)
	assert.Equal(t, "default", release.Namespace)
	assert.Equal(t, "my-chart", release.Chart)
	assert.Equal(t, "1.2.3", release.Version)
	assert.Equal(t, "4.5.6", release.AppVer)
	assert.Equal(t, "deployed", release.Status)
	assert.Equal(t, 3, release.Revision)
}

func TestParseHelmSecret_WrongType(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "not-helm"},
		Type:       "Opaque",
		Data:       map[string][]byte{"release": []byte("data")},
	}
	release := ParseHelmSecret(secret)
	assert.Nil(t, release)
}

func TestParseHelmSecret_MissingReleaseKey(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "no-release-key"},
		Type:       "helm.sh/release.v1",
		Data:       map[string][]byte{"other": []byte("data")},
	}
	release := ParseHelmSecret(secret)
	assert.Nil(t, release)
}

func TestParseHelmSecret_InvalidGzip(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-gzip"},
		Type:       "helm.sh/release.v1",
		Data:       map[string][]byte{"release": []byte("not-valid-gzip-or-base64")},
	}
	release := ParseHelmSecret(secret)
	assert.Nil(t, release)
}

// --- TriggerOpenShiftUpgrade tests ---

func TestTriggerOpenShiftUpgrade_MissingTargetVersion(t *testing.T) {
	ca := &mockClusterAccess{}
	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Equal(t, "target_version is required", result)
}

func TestTriggerOpenShiftUpgrade_MissingConfirmation(t *testing.T) {
	ca := &mockClusterAccess{}
	args := map[string]interface{}{
		"target_version": "4.14.5",
	}
	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, args)
	assert.False(t, isErr)
	assert.Contains(t, result, "Safety Check Failed")
	assert.Contains(t, result, "confirm='yes-upgrade-now'")
}

func TestTriggerOpenShiftUpgrade_WrongConfirmation(t *testing.T) {
	ca := &mockClusterAccess{}
	args := map[string]interface{}{
		"target_version": "4.14.5",
		"confirm":        "yes",
	}
	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, args)
	assert.False(t, isErr)
	assert.Contains(t, result, "Safety Check Failed")
}

func TestTriggerOpenShiftUpgrade_ClientError(t *testing.T) {
	ca := &mockClusterAccess{
		dynErr: fmt.Errorf("connection refused"),
	}
	args := map[string]interface{}{
		"target_version": "4.14.5",
		"confirm":        "yes-upgrade-now",
	}
	result, isErr := TriggerOpenShiftUpgrade(context.Background(), ca, args)
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create client")
}

// --- DetectClusterType tests ---

func TestDetectClusterType_ClientError(t *testing.T) {
	ca := &mockClusterAccess{
		clientErr: fmt.Errorf("no config"),
	}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create client")
}

func TestDetectClusterType_DynClientError(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")
	ca := &mockClusterAccess{
		client: cs,
		dynErr: fmt.Errorf("dyn error"),
	}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create dynamic client")
}

func TestDetectClusterType_K3s(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0+k3s1")

	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("not found")
	})

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-1",
			Labels: map[string]string{"kubernetes.io/hostname": "node-1"},
		},
	}
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeK3s)
}

func TestDetectClusterType_Kind(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("not found")
	})

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "kind-control-plane",
			Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
		},
	}
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeKind)
}

func TestDetectClusterType_OpenShift(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion",
	}, &unstructured.Unstructured{})

	cvObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "ClusterVersion",
			"metadata":   map[string]interface{}{"name": "version"},
		},
	}
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme, cvObj)

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeOpenShift)
}

func TestDetectClusterType_Unknown(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("not found")
	})

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "generic-node",
			Labels: map[string]string{"kubernetes.io/hostname": "generic-node"},
		},
	}
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, ClusterTypeUnknown)
}

// --- GetClusterVersionInfo tests ---

func TestGetClusterVersionInfo_ClientError(t *testing.T) {
	ca := &mockClusterAccess{clientErr: fmt.Errorf("no config")}
	result, isErr := GetClusterVersionInfo(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create client")
}

func TestGetClusterVersionInfo_VanillaKubernetes(t *testing.T) {
	cs := newFakeClientWithVersion("v1.29.2")

	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("not found")
	})

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.29.2"},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetClusterVersionInfo(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, "v1.29.2")
	assert.Contains(t, result, "Kubernetes")
}

// --- GetUpgradePrerequisites tests ---

func TestGetUpgradePrerequisites_ClientError(t *testing.T) {
	ca := &mockClusterAccess{clientErr: fmt.Errorf("no config")}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create client")
}

func TestGetUpgradePrerequisites_HealthyCluster(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	cs := newFakeClientWithVersion("v1.29.0", node)

	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("not found")
	})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	result, isErr := GetUpgradePrerequisites(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, "All nodes ready (1/1)")
	assert.Contains(t, result, "Passed")
}

// --- CheckHelmReleaseUpgrades tests ---

func TestCheckHelmReleaseUpgrades_ClientError(t *testing.T) {
	ca := &mockClusterAccess{clientErr: fmt.Errorf("no config")}
	result, isErr := CheckHelmReleaseUpgrades(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create client")
}

func TestCheckHelmReleaseUpgrades_NoReleases(t *testing.T) {
	cs := kubefake.NewSimpleClientset()
	ca := &mockClusterAccess{client: cs}
	result, isErr := CheckHelmReleaseUpgrades(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, "Helm Releases Found:** 0")
}

func TestCheckHelmReleaseUpgrades_WithReleases(t *testing.T) {
	secret := makeHelmSecret("nginx", "default", "nginx-ingress", "4.7.1", "1.9.0", "deployed", 2)
	cs := kubefake.NewSimpleClientset(secret)
	ca := &mockClusterAccess{client: cs}
	result, isErr := CheckHelmReleaseUpgrades(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.Contains(t, result, "nginx")
	assert.Contains(t, result, "4.7.1")
	assert.Contains(t, result, "deployed")
}

func TestCheckHelmReleaseUpgrades_WithNamespace(t *testing.T) {
	secret := makeHelmSecret("cert-manager", "cert-manager", "cert-manager", "1.14.0", "1.14.0", "deployed", 1)
	cs := kubefake.NewSimpleClientset(secret)
	ca := &mockClusterAccess{client: cs}
	result, isErr := CheckHelmReleaseUpgrades(context.Background(), ca, map[string]interface{}{
		"namespace": "cert-manager",
	})
	assert.False(t, isErr)
	assert.Contains(t, result, "cert-manager")
}

// --- CheckOLMOperatorUpgrades tests ---

func TestCheckOLMOperatorUpgrades_ClientError(t *testing.T) {
	ca := &mockClusterAccess{dynErr: fmt.Errorf("no config")}
	result, isErr := CheckOLMOperatorUpgrades(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create client")
}

// --- GetUpgradeStatus tests ---

func TestGetUpgradeStatus_ClientError(t *testing.T) {
	ca := &mockClusterAccess{dynErr: fmt.Errorf("no config")}
	result, isErr := GetUpgradeStatus(context.Background(), ca, map[string]interface{}{})
	assert.True(t, isErr)
	assert.Contains(t, result, "Failed to create dynamic client")
}

// --- Tools() registration tests ---

func TestToolsRegistration(t *testing.T) {
	tools := Tools()
	assert.Greater(t, len(tools), 0, "Tools() should return at least one tool")

	names := make(map[string]bool)
	for _, tool := range tools {
		assert.NotEmpty(t, tool.Schema.Name, "Tool name must not be empty")
		assert.NotEmpty(t, tool.Schema.Description, "Tool %s must have a description", tool.Schema.Name)
		assert.NotNil(t, tool.Handler, "Tool %s must have a non-nil handler", tool.Schema.Name)

		// No duplicate names
		assert.False(t, names[tool.Schema.Name], "Duplicate tool name: %s", tool.Schema.Name)
		names[tool.Schema.Name] = true
	}
}

func TestToolsContainExpectedSet(t *testing.T) {
	tools := Tools()
	expected := []string{
		"detect_cluster_type",
		"get_cluster_version_info",
		"check_olm_operator_upgrades",
		"check_helm_release_upgrades",
		"get_upgrade_prerequisites",
		"trigger_openshift_upgrade",
		"get_upgrade_status",
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Schema.Name] = true
	}

	for _, exp := range expected {
		assert.True(t, names[exp], "Expected tool %q not found in Tools()", exp)
	}
}

func TestTriggerOpenShiftUpgrade_RequiredFields(t *testing.T) {
	tools := Tools()
	for _, tool := range tools {
		if tool.Schema.Name == "trigger_openshift_upgrade" {
			assert.Contains(t, tool.Schema.InputSchema.Required, "target_version")
			assert.Contains(t, tool.Schema.InputSchema.Required, "confirm")
			return
		}
	}
	t.Fatal("trigger_openshift_upgrade tool not found")
}

// --- Edge case: ParseHelmSecret with multiple revisions ---

func TestParseHelmSecret_HigherRevisionWins(t *testing.T) {
	s1 := makeHelmSecret("app", "ns", "chart", "1.0.0", "1.0", "superseded", 1)
	s2 := makeHelmSecret("app", "ns", "chart", "2.0.0", "2.0", "deployed", 2)

	r1 := ParseHelmSecret(s1)
	r2 := ParseHelmSecret(s2)

	require.NotNil(t, r1)
	require.NotNil(t, r2)
	assert.Equal(t, 1, r1.Revision)
	assert.Equal(t, 2, r2.Revision)
	assert.Equal(t, "2.0.0", r2.Version)
}

// --- Confirm string safety on DetectClusterType ---

func TestDetectClusterType_EmptyArgs(t *testing.T) {
	cs := newFakeClientWithVersion("v1.28.0")

	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	dynClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("not found")
	})

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-1",
			Labels: map[string]string{"kubernetes.io/hostname": "node-1"},
		},
	}
	_, _ = cs.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	ca := &mockClusterAccess{client: cs, dynClient: dynClient}
	// Empty args should not panic (cluster defaults to "")
	result, isErr := DetectClusterType(context.Background(), ca, map[string]interface{}{})
	assert.False(t, isErr)
	assert.True(t, len(result) > 0)
	_ = strings.Contains(result, "Cluster Type") // just verifying no crash
}
