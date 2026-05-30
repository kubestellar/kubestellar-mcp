package server

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "seconds", when: time.Now().Add(-30 * time.Second), want: "30s"},
		{name: "minutes", when: time.Now().Add(-(2*time.Minute + 10*time.Second)), want: "2m"},
		{name: "hours", when: time.Now().Add(-(3*time.Hour + 5*time.Minute)), want: "3h"},
		{name: "days", when: time.Now().Add(-(49 * time.Hour)), want: "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatAge(tt.when); got != tt.want {
				t.Fatalf("formatAge() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormattingHelpers(t *testing.T) {
	ports := formatPorts([]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}, {Port: 443, NodePort: 30443, Protocol: corev1.ProtocolTCP}})
	if ports != "80/TCP,443:30443/TCP" {
		t.Fatalf("formatPorts() = %q", ports)
	}

	subjects := []rbacv1.Subject{
		{Kind: "ServiceAccount", Namespace: "apps", Name: "builder"},
		{Kind: "User", Name: "alice"},
		{Kind: "Group", Name: "admins"},
	}
	formattedSubjects := formatSubjects(subjects)
	for _, want := range []string{"SA:apps/builder", "User:alice", "Group:admins"} {
		if !strings.Contains(formattedSubjects, want) {
			t.Fatalf("formatSubjects() missing %q in %q", want, formattedSubjects)
		}
	}

	if !subjectMatches(subjects, "ServiceAccount", "builder", "apps") {
		t.Fatal("subjectMatches() should match service account with namespace")
	}
	if subjectMatches(subjects, "ServiceAccount", "builder", "other") {
		t.Fatal("subjectMatches() should reject service account in wrong namespace")
	}
	if !subjectMatches(subjects, "User", "alice", "") {
		t.Fatal("subjectMatches() should match user without namespace")
	}
}

func TestFormatMultiClusterResults(t *testing.T) {
	results := []ClusterResult{{Cluster: "alpha", Result: "ok"}, {Cluster: "beta", Error: "boom"}}
	formatted := formatMultiClusterResults(results)

	var decoded []ClusterResult
	if err := json.Unmarshal([]byte(formatted), &decoded); err != nil {
		t.Fatalf("formatMultiClusterResults() produced invalid JSON: %v", err)
	}
	if len(decoded) != 2 || decoded[1].Cluster != "beta" || decoded[1].Error != "boom" {
		t.Fatalf("unexpected decoded results: %#v", decoded)
	}
}

func TestParseHelmSecret(t *testing.T) {
	release := map[string]interface{}{
		"name":    "demo",
		"version": float64(7),
		"info":    map[string]interface{}{"status": "deployed"},
		"chart": map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":       "demo-chart",
				"version":    "1.2.3",
				"appVersion": "4.5.6",
			},
		},
	}

	s := &Server{}
	for _, tc := range []struct {
		name   string
		secret *corev1.Secret
	}{
		{name: "base64 wrapped", secret: newHelmSecret(t, release, true)},
		{name: "raw gzipped", secret: newHelmSecret(t, release, false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed := s.parseHelmSecret(tc.secret)
			if parsed == nil {
				t.Fatal("parseHelmSecret() returned nil")
			}
			if parsed.Name != "demo" || parsed.Namespace != "ops" || parsed.Chart != "demo-chart" || parsed.Version != "1.2.3" || parsed.AppVer != "4.5.6" || parsed.Status != "deployed" || parsed.Revision != 7 {
				t.Fatalf("unexpected parsed release: %#v", parsed)
			}
		})
	}

	if got := s.parseHelmSecret(&corev1.Secret{Type: corev1.SecretTypeOpaque}); got != nil {
		t.Fatalf("parseHelmSecret() for non-helm secret = %#v, want nil", got)
	}
}

func newHelmSecret(t *testing.T, release map[string]interface{}, wrapBase64 bool) *corev1.Secret {
	t.Helper()
	payload, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("failed to marshal release: %v", err)
	}

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(payload); err != nil {
		t.Fatalf("failed to compress release: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to finalize gzip payload: %v", err)
	}

	data := compressed.Bytes()
	if wrapBase64 {
		data = []byte(base64.StdEncoding.EncodeToString(data))
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ops",
		},
		Type: "helm.sh/release.v1",
		Data: map[string][]byte{"release": data},
	}
}
