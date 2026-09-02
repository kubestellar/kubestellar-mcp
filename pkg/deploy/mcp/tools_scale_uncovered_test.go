package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleScaleAppRejectsInvalidJSON exercises the json.Unmarshal error arm
// of handleScaleApp (tools_deploy.go:423) which existing tests skip.
func TestHandleScaleAppRejectsInvalidJSON(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleScaleApp(context.Background(), []byte(`{invalid`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid arguments")
}

// TestHandleScaleAppRejectsBlockedNamespace exercises the namespace-validation
// error arm — the guard at tools_deploy.go that refuses scaling AI-driven
// operations against system namespaces such as kube-system.
func TestHandleScaleAppRejectsBlockedNamespace(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleScaleApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"app":       "demo",
		"replicas":  3,
		"namespace": "kube-system",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid namespace")
	assert.Contains(t, err.Error(), "kube-system")
}

// TestHandleScaleAppRejectsMalformedNamespace exercises the RFC 1123 regex
// arm of ValidateNamespace via handleScaleApp — an uppercase namespace fails
// the regex check before the blocklist check.
func TestHandleScaleAppRejectsMalformedNamespace(t *testing.T) {
	server := newHelmTestServer(t, map[string]string{})

	_, err := server.handleScaleApp(context.Background(), mustMarshalJSON(t, map[string]interface{}{
		"app":       "demo",
		"replicas":  3,
		"namespace": "BadNamespace",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid namespace")
}
