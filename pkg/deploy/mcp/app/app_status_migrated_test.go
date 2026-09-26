package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAppStatus_InvalidAppName_Migrated(t *testing.T) {
	_, err := GetAppStatus(context.Background(), &fakeExecutor{}, mustMarshalJSON(t, map[string]interface{}{"app": "bad;name"}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid app name")
}

func TestGetAppStatus_InvalidNamespace_Migrated(t *testing.T) {
	cases := []struct {
		name      string
		namespace string
	}{
		{name: "invalid format", namespace: "Invalid_NS"},
		{name: "protected namespace", namespace: "kube-system"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GetAppStatus(context.Background(), &fakeExecutor{}, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": tc.namespace}))
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid namespace")
		})
	}
}
