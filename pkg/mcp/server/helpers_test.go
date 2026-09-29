package server

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	for _, tc := range []struct {
		name   string
		secret *corev1.Secret
	}{
		{name: "base64 wrapped", secret: newHelmSecret(t, release, true)},
		{name: "raw gzipped", secret: newHelmSecret(t, release, false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseHelmSecret(tc.secret)
			if parsed == nil {
				t.Fatal("parseHelmSecret() returned nil")
			}
			if parsed.Name != "demo" || parsed.Namespace != "ops" || parsed.Chart != "demo-chart" || parsed.Version != "1.2.3" || parsed.AppVer != "4.5.6" || parsed.Status != "deployed" || parsed.Revision != 7 {
				t.Fatalf("unexpected parsed release: %#v", parsed)
			}
		})
	}

	if got := parseHelmSecret(&corev1.Secret{Type: corev1.SecretTypeOpaque}); got != nil {
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
