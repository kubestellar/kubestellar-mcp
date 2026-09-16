package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	server "github.com/kubestellar/kubestellar-mcp/pkg/mcp/server"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

// sensitiveKinds enumerates resource kinds the MCP kubectl surface must NOT
// mutate on the caller's behalf. The block is scoped by the invariant that
// creating any listed resource is equivalent to privilege escalation on the
// target cluster/namespace — either directly (Secret/ServiceAccount/RBAC),
// via admission-time rewrite (Mutating/ValidatingWebhookConfiguration), or via
// out-of-band credential minting (CertificateSigningRequest, legacy PSP).
//
// Namespaced Role/RoleBinding are included because they can grant `secrets:*`
// (and thereby defeat the Secret block) with a single follow-up apply — see
// issue #804.
var sensitiveKinds = map[string]bool{
	// Cluster-scoped RBAC
	"clusterrole":         true,
	"clusterroles":        true,
	"clusterrolebinding":  true,
	"clusterrolebindings": true,
	// Namespaced RBAC (grants that equal Secret access via one extra apply)
	"role":         true,
	"roles":        true,
	"rolebinding":  true,
	"rolebindings": true,
	// Credentials
	"secret":          true,
	"secrets":         true,
	"serviceaccount":  true,
	"serviceaccounts": true,
	"sa":              true,
	// Admission-time in-cluster code execution
	"mutatingwebhookconfiguration":    true,
	"mutatingwebhookconfigurations":   true,
	"validatingwebhookconfiguration":  true,
	"validatingwebhookconfigurations": true,
	// Cert minting
	"certificatesigningrequest":  true,
	"certificatesigningrequests": true,
	"csr":                        true,
	// Legacy but still present on older clusters
	"podsecuritypolicy":   true,
	"podsecuritypolicies": true,
	"psp":                 true,
}

func isSensitiveKind(kind string) bool {
	return sensitiveKinds[strings.ToLower(kind)]
}

func isNamespaceKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "namespace", "namespaces", "ns":
		return true
	default:
		return false
	}
}

func sensitiveKindError(kind string) error {
	return fmt.Errorf("%q resources are blocked via MCP kubectl tools to prevent privilege escalation; use kubectl directly for this sensitive operation", kind)
}

func manifestSensitiveKind(doc string) (string, bool) {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return "", false
	}

	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON([]byte(yamlToJSON(doc))); err != nil {
		if err := unstructuredFromYAML(doc, obj); err != nil {
			return "", false
		}
	}

	kind := obj.GetKind()
	return kind, isSensitiveKind(kind)
}

// yamlToJSON converts YAML or JSON strings to JSON for Kubernetes decoding.
func yamlToJSON(yamlStr string) string {
	data, err := yamlToJSONBytes([]byte(yamlStr))
	if err != nil {
		return yamlStr
	}
	return string(data)
}

// unstructuredFromYAML parses YAML into an Unstructured object
func unstructuredFromYAML(yamlStr string, obj *unstructured.Unstructured) error {
	// Use k8s.io/apimachinery/pkg/util/yaml for proper parsing
	data, err := yamlToJSONBytes([]byte(yamlStr))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}

// yamlToJSONBytes converts YAML bytes to JSON bytes.
func yamlToJSONBytes(y []byte) ([]byte, error) {
	return k8syaml.ToJSON(y)
}

// parseYAML parses YAML or JSON manifests into the provided value.
func parseYAML(data []byte, v interface{}) error {
	jsonData, err := yamlToJSONBytes(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonData, v)
}

// validateManifestDocs validates every --- separated document in a built manifest,
// enforcing the same sensitive-kind and namespace rules used by the kubectl handlers.
// Called by kustomize handlers before piping built output to kubectl apply/delete.
func validateManifestDocs(manifest string) error {
	for _, doc := range strings.Split(manifest, "---") {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		if kind, blocked := manifestSensitiveKind(doc); blocked {
			return sensitiveKindError(kind)
		}
		obj := &unstructured.Unstructured{}
		if err := unstructuredFromYAML(doc, obj); err == nil {
			if isNamespaceKind(obj.GetKind()) {
				if err := server.ValidateNamespace(obj.GetName()); err != nil {
					return fmt.Errorf("invalid namespace in manifest: %w", err)
				}
			} else if ns := obj.GetNamespace(); ns != "" {
				if err := server.ValidateNamespace(ns); err != nil {
					return fmt.Errorf("invalid namespace in manifest: %w", err)
				}
			}
		}
	}
	return nil
}
