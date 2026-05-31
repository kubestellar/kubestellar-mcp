package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	kubernetes "k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestToolRBACAndResourceOutputs(t *testing.T) {
	created := metav1.NewTime(time.Date(2024, time.February, 3, 4, 5, 6, 0, time.UTC))

	client := k8sfake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "apps"},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeNodePort,
				ClusterIP: "10.0.0.1",
				Ports:     []corev1.ServicePort{{Port: 80, NodePort: 30080, Protocol: corev1.ProtocolTCP}},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1", Labels: map[string]string{"node-role.kubernetes.io/worker": ""}},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
				NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: "v1.30.0"},
			},
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "event-1", Namespace: "apps"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "demo"},
			Type:           "Warning",
			Message:        "Back-off restarting failed container",
		},
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "reader", Namespace: "apps", CreationTimestamp: created},
			Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}}},
		},
		&rbacv1.ClusterRole{
			ObjectMeta:      metav1.ObjectMeta{Name: "platform-admin", CreationTimestamp: created},
			Rules:           []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods", "services"}, Verbs: []string{"get", "list", "watch"}}},
			AggregationRule: &rbacv1.AggregationRule{ClusterRoleSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"rbac.example.com/aggregate": "true"}}}},
		},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "system:basic-user"}},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "reader-binding", Namespace: "apps"},
			RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "reader"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "apps", Name: "default"}, {Kind: "User", Name: "alice"}},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "platform-admin-binding"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "platform-admin"},
			Subjects:   []rbacv1.Subject{{Kind: "User", Name: "alice"}},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "system:discovery"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "system:discovery"},
			Subjects:   []rbacv1.Subject{{Kind: "Group", Name: "system:authenticated"}},
		},
	)
	client.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authorizationv1.SelfSubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{Allowed: false, Reason: "policy denied"},
		}, nil
	})

	server := &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
		if clusterName != "" && clusterName != "alpha" {
			t.Fatalf("unexpected cluster %q", clusterName)
		}
		return client, nil
	}}

	assertToolText(t, server, "get_services", map[string]interface{}{"cluster": "alpha", "namespace": "apps"}, []string{"Found 1 services:", "apps/frontend", "NodePort", "10.0.0.1", "80:30080/TCP"})
	assertToolText(t, server, "get_nodes", map[string]interface{}{"cluster": "alpha"}, []string{"Found 1 nodes:", "worker-1", "Ready", "worker", "v1.30.0"})
	assertToolText(t, server, "get_events", map[string]interface{}{"cluster": "alpha", "namespace": "apps", "limit": 10.0}, []string{"Found 1 events:", "[Warning] Pod/demo", "Back-off restarting failed container"})
	assertToolText(t, server, "get_roles", map[string]interface{}{"cluster": "alpha", "namespace": "apps"}, []string{"Found 1 roles:", "apps/reader", "1 rules"})
	assertToolText(t, server, "get_cluster_roles", map[string]interface{}{"cluster": "alpha"}, []string{"Found 1 cluster roles:", "platform-admin", "1 rules", "(aggregated)"})
	assertToolNotContains(t, server, "get_cluster_roles", map[string]interface{}{"cluster": "alpha"}, []string{"system:basic-user"})
	assertToolText(t, server, "get_role_bindings", map[string]interface{}{"cluster": "alpha", "namespace": "apps"}, []string{"Found 1 role bindings:", "apps/reader-binding", "Role/reader", "SA:apps/default", "User:alice"})
	assertToolText(t, server, "get_cluster_role_bindings", map[string]interface{}{"cluster": "alpha"}, []string{"Found 1 cluster role bindings:", "platform-admin-binding", "User:alice"})
	assertToolNotContains(t, server, "get_cluster_role_bindings", map[string]interface{}{"cluster": "alpha"}, []string{"system:discovery"})
	assertToolText(t, server, "can_i", map[string]interface{}{"cluster": "alpha", "verb": "delete", "resource": "pods", "namespace": "apps", "name": "demo"}, []string{"Can I delete pods in namespace apps (name: demo)?", "✗ No, access is denied", "Reason: policy denied"})
	assertToolText(t, server, "analyze_subject_permissions", map[string]interface{}{"cluster": "alpha", "subject_kind": "User", "subject_name": "alice"}, []string{"RBAC Analysis for User: alice", "Cluster-wide permissions via ClusterRoleBindings:", "platform-admin:", "get, list, watch on pods, services", "Namespace-scoped permissions via RoleBindings:", "Namespace apps: reader (Role)"})
	assertToolText(t, server, "describe_role", map[string]interface{}{"cluster": "alpha", "namespace": "apps", "name": "reader"}, []string{"Role: apps/reader", "Rules:", "API Groups:", "Resources: pods", "Verbs: get, list"})
	assertToolText(t, server, "describe_role", map[string]interface{}{"cluster": "alpha", "name": "platform-admin"}, []string{"ClusterRole: platform-admin", "Aggregation Rule: yes", "API Groups: core", "Resources: pods, services", "Verbs: get, list, watch"})
}

func TestToolFindResourceOwnersReportsManagersAndLabels(t *testing.T) {
	managedAt := metav1.NewTime(time.Date(2024, time.March, 4, 5, 6, 7, 0, time.UTC))
	client := k8sfake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "frontend-pod",
				Namespace:       "apps",
				Labels:          map[string]string{"app.kubernetes.io/managed-by": "argo", "team": "platform", "created-by": "alice"},
				ManagedFields:   []metav1.ManagedFieldsEntry{{Manager: "kubectl", Time: &managedAt}},
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "frontend-rs"}},
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:          "frontend",
				Namespace:     "apps",
				Annotations:   map[string]string{"meta.helm.sh/release-name": "frontend-release"},
				ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "helm", Time: &managedAt}},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "frontend-svc",
				Namespace: "apps",
				Labels:    map[string]string{"owner": "bob"},
			},
		},
	)

	server := &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) { return client, nil }}
	assertToolText(t, server, "find_resource_owners", map[string]interface{}{"namespace": "apps"}, []string{
		"# Resource Ownership in namespace: apps",
		"Found 3 resources",
		"## By Manager/Controller",
		"### kubectl",
		"**Pod/frontend-pod** (owner: ReplicaSet/frontend-rs) [managed-by: argo] [team: platform] [created-by: alice]",
		"### helm",
		"**Deployment/frontend** [managed-by: helm:frontend-release]",
		"### (unknown)",
		"| Kind | Name | Manager | Owner | Managed-By | Team | Last Update |",
		"| Pod | frontend-pod | kubectl | ReplicaSet/frontend-rs | argo | platform | 2024-03-04 05:06:07 |",
		"| Deployment | frontend | helm | - | helm:frontend-release | - | 2024-03-04 05:06:07 |",
	})
}

func TestGatekeeperAndOwnershipPolicyTools(t *testing.T) {
	ctGVR := schema.GroupVersionResource{Group: "templates.gatekeeper.sh", Version: "v1", Resource: "constrainttemplates"}
	constraintGVR := schema.GroupVersionResource{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: "k8srequiredlabels"}

	dynamicClient := newDynamicClient(t, ctGVR, constraintGVR,
		newUnstructured(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1", Kind: "ConstraintTemplate"}, ownershipTemplateName, map[string]interface{}{
			"status": map[string]interface{}{"created": true},
		}),
		newUnstructured(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1", Kind: "ConstraintTemplate"}, "other-template", nil),
		newUnstructured(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"}, ownershipConstraintName, map[string]interface{}{
			"spec": map[string]interface{}{
				"enforcementAction": "warn",
				"parameters":        map[string]interface{}{"labels": []interface{}{"owner", "team"}},
				"match":             map[string]interface{}{"excludedNamespaces": []interface{}{"kube-system"}},
			},
			"status": map[string]interface{}{
				"totalViolations": int64(2),
				"violations": []interface{}{
					map[string]interface{}{"namespace": "apps", "kind": "Pod", "name": "frontend", "message": strings.Repeat("missing owner label ", 4)},
					map[string]interface{}{"namespace": "test", "kind": "Service", "name": "backend", "message": "missing team label"},
				},
			},
		}),
	)

	client := k8sfake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: gatekeeperNamespace}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-controller-manager-0", Namespace: gatekeeperNamespace},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}},
		},
	)

	server := &Server{
		clientFactory:        func(clusterName string) (kubernetes.Interface, error) { return client, nil },
		dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) { return dynamicClient, nil },
	}

	assertToolText(t, server, "check_gatekeeper", map[string]interface{}{"cluster": "alpha"}, []string{"# OPA Gatekeeper Status", "**Status:** Installed and Healthy ✓", "**Pods:** 1/1 running", "**ConstraintTemplates:** 2 installed", "- other-template", "**Ownership Policy:** Installed (template: k8srequiredlabels)"})
	assertToolText(t, server, "get_ownership_policy_status", map[string]interface{}{"cluster": "alpha"}, []string{"# Ownership Policy Status", "**Template:** k8srequiredlabels (created: true)", "**Constraint:** require-ownership-labels", "**Mode:** warn", "**Required Labels:** owner, team", "**Excluded Namespaces:** kube-system", "**Total Violations:** 2"})
	assertToolText(t, server, "list_ownership_violations", map[string]interface{}{"cluster": "alpha", "limit": 1.0}, []string{"# Ownership Label Violations", "**Mode:** warn", "**Total Violations:** 2", "## By Namespace", "- **apps**: 1 violations", "- **test**: 1 violations", "| apps | Pod | frontend |", "*Showing 1 of 2 violations. Use `limit` parameter to see more.*"})
	assertToolText(t, server, "list_ownership_violations", map[string]interface{}{"cluster": "alpha", "namespace": "apps"}, []string{"- **apps**: 1 violations", "| apps | Pod | frontend |"})
	assertToolNotContains(t, server, "list_ownership_violations", map[string]interface{}{"cluster": "alpha", "namespace": "apps"}, []string{"test | Service | backend"})
}

func newDynamicClient(t *testing.T, ctGVR, constraintGVR schema.GroupVersionResource, objs ...runtime.Object) dynamic.Interface {
	t.Helper()
	resources := map[schema.GroupVersionResource]map[string]*unstructured.Unstructured{
		ctGVR:         {},
		constraintGVR: {},
	}
	for _, obj := range objs {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			t.Fatalf("unexpected object type %T", obj)
		}
		gvk := u.GroupVersionKind()
		resource := schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version}
		switch gvk.Kind {
		case "ConstraintTemplate":
			resource.Resource = ctGVR.Resource
		case "K8sRequiredLabels":
			resource.Resource = constraintGVR.Resource
		default:
			t.Fatalf("unexpected GVK %s", gvk.String())
		}
		resources[resource][u.GetName()] = u.DeepCopy()
	}
	return stubDynamicClient{resources: resources}
}

func newUnstructured(gvk schema.GroupVersionKind, name string, extra map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": name},
	}}
	obj.SetGroupVersionKind(gvk)
	for key, value := range extra {
		obj.Object[key] = value
	}
	return obj
}

func assertToolText(t *testing.T, server *Server, tool string, args map[string]interface{}, want []string) {
	t.Helper()
	result, rpcErr := callTool(t, server, tool, args)
	if rpcErr != nil {
		t.Fatalf("callTool(%s) returned RPC error: %v", tool, rpcErr)
	}
	if len(result.Content) != 1 {
		t.Fatalf("callTool(%s) content length = %d, want 1", tool, len(result.Content))
	}
	for _, needle := range want {
		if !strings.Contains(result.Content[0].Text, needle) {
			t.Fatalf("callTool(%s) output %q missing %q", tool, result.Content[0].Text, needle)
		}
	}
}

func assertToolNotContains(t *testing.T, server *Server, tool string, args map[string]interface{}, unwanted []string) {
	t.Helper()
	result, rpcErr := callTool(t, server, tool, args)
	if rpcErr != nil {
		t.Fatalf("callTool(%s) returned RPC error: %v", tool, rpcErr)
	}
	if len(result.Content) != 1 {
		t.Fatalf("callTool(%s) content length = %d, want 1", tool, len(result.Content))
	}
	for _, needle := range unwanted {
		if strings.Contains(result.Content[0].Text, needle) {
			t.Fatalf("callTool(%s) output %q unexpectedly contained %q", tool, result.Content[0].Text, needle)
		}
	}
}

func TestGetDynamicClientForClusterUsesFactory(t *testing.T) {
	want := newDynamicClient(t,
		schema.GroupVersionResource{Group: "templates.gatekeeper.sh", Version: "v1", Resource: "constrainttemplates"},
		schema.GroupVersionResource{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: "k8srequiredlabels"},
		newUnstructured(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1", Kind: "ConstraintTemplate"}, ownershipTemplateName, nil),
	)
	server := &Server{dynamicClientFactory: func(clusterName string) (dynamic.Interface, error) {
		if clusterName != "alpha" {
			t.Fatalf("clusterName = %q, want alpha", clusterName)
		}
		return want, nil
	}}

	got, err := server.getDynamicClientForCluster("alpha")
	if err != nil {
		t.Fatalf("getDynamicClientForCluster returned error: %v", err)
	}
	if got == nil {
		t.Fatal("getDynamicClientForCluster returned nil client")
	}
}

type stubDynamicClient struct {
	resources map[schema.GroupVersionResource]map[string]*unstructured.Unstructured
}

func (c stubDynamicClient) Resource(resource schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return stubDynamicResource{resource: resource, objects: c.resources[resource]}
}

type stubDynamicResource struct {
	resource schema.GroupVersionResource
	objects  map[string]*unstructured.Unstructured
}

func (r stubDynamicResource) Namespace(string) dynamic.ResourceInterface { return r }

func (r stubDynamicResource) Create(context.Context, *unstructured.Unstructured, metav1.CreateOptions, ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}

func (r stubDynamicResource) Update(context.Context, *unstructured.Unstructured, metav1.UpdateOptions, ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}

func (r stubDynamicResource) UpdateStatus(context.Context, *unstructured.Unstructured, metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}

func (r stubDynamicResource) Delete(context.Context, string, metav1.DeleteOptions, ...string) error {
	return errors.New("not implemented")
}

func (r stubDynamicResource) DeleteCollection(context.Context, metav1.DeleteOptions, metav1.ListOptions) error {
	return errors.New("not implemented")
}

func (r stubDynamicResource) Get(_ context.Context, name string, _ metav1.GetOptions, _ ...string) (*unstructured.Unstructured, error) {
	if obj, ok := r.objects[name]; ok {
		return obj.DeepCopy(), nil
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: r.resource.Group, Resource: r.resource.Resource}, name)
}

func (r stubDynamicResource) List(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	items := make([]unstructured.Unstructured, 0, len(r.objects))
	for _, obj := range r.objects {
		items = append(items, *obj.DeepCopy())
	}
	return &unstructured.UnstructuredList{Items: items}, nil
}

func (r stubDynamicResource) Watch(context.Context, metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.New("not implemented")
}

func (r stubDynamicResource) Patch(context.Context, string, types.PatchType, []byte, metav1.PatchOptions, ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}

func (r stubDynamicResource) Apply(context.Context, string, *unstructured.Unstructured, metav1.ApplyOptions, ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}

func (r stubDynamicResource) ApplyStatus(context.Context, string, *unstructured.Unstructured, metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}

func TestToolGetServicesAndNodesUseContext(t *testing.T) {
	server := &Server{clientFactory: func(clusterName string) (kubernetes.Interface, error) {
		if clusterName != "alpha" {
			t.Fatalf("clusterName = %q, want alpha", clusterName)
		}
		return k8sfake.NewSimpleClientset(), nil
	}}

	if _, isErr := server.toolGetServices(context.Background(), map[string]interface{}{"cluster": "alpha"}); isErr {
		t.Fatal("toolGetServices returned unexpected error")
	}
	if _, isErr := server.toolGetNodes(context.Background(), map[string]interface{}{"cluster": "alpha"}); isErr {
		t.Fatal("toolGetNodes returned unexpected error")
	}
}
