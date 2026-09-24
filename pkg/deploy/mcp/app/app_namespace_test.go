package app

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleAppNamespaceValidation(t *testing.T) {
	executor := &fakeExecutor{}
	cases := []struct {
		name string
		call func() error
	}{
		{name: "instances blocked system namespace", call: func() error {
			_, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "kube-system"}))
			return err
		}},
		{name: "instances openshift namespace", call: func() error {
			_, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "openshift-monitoring"}))
			return err
		}},
		{name: "instances invalid namespace format", call: func() error {
			_, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "Invalid_NS"}))
			return err
		}},
		{name: "status blocked system namespace", call: func() error {
			_, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "kube-public"}))
			return err
		}},
		{name: "status gatekeeper namespace", call: func() error {
			_, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "gatekeeper-system"}))
			return err
		}},
		{name: "logs blocked system namespace", call: func() error {
			_, err := GetAppLogs(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "kube-node-lease"}))
			return err
		}},
		{name: "logs openshift namespace", call: func() error {
			_, err := GetAppLogs(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "openshift-logging"}))
			return err
		}},
		{name: "logs invalid namespace format", call: func() error {
			_, err := GetAppLogs(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo", "namespace": "Invalid_NS"}))
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid namespace")
		})
	}
}

func TestHandleAppInvalidAppName(t *testing.T) {
	executor := &fakeExecutor{}
	cases := []struct {
		name string
		call func() error
	}{
		{name: "instances empty app", call: func() error {
			_, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": ""}))
			return err
		}},
		{name: "instances uppercase app", call: func() error {
			_, err := GetAppInstances(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "MyApp"}))
			return err
		}},
		{name: "status empty app", call: func() error {
			_, err := GetAppStatus(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": ""}))
			return err
		}},
		{name: "logs empty app", call: func() error {
			_, err := GetAppLogs(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": ""}))
			return err
		}},
		{name: "logs shell injection app", call: func() error {
			_, err := GetAppLogs(context.Background(), executor, mustMarshalJSON(t, map[string]interface{}{"app": "demo;rm -rf /"}))
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), "invalid app name"))
		})
	}
}
