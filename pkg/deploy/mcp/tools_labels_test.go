package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildLabelPatch(t *testing.T) {
	addPatch := decodeLabelPatch(t, buildLabelPatch(map[string]string{"env": "prod"}, false))
	labels := addPatch["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
	if labels["env"] != "prod" {
		t.Fatalf("add patch labels = %#v", labels)
	}

	removePatch := decodeLabelPatch(t, buildLabelPatch(map[string]string{"env": "ignored"}, true))
	removeLabels := removePatch["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
	if value, exists := removeLabels["env"]; !exists || value != nil {
		t.Fatalf("remove patch labels = %#v, want env=null", removeLabels)
	}
}

func TestLabelOperationsDryRunAndUnsupportedKinds(t *testing.T) {
	s := &Server{}

	addResult, err := s.addLabelsInCluster(context.Background(), nil, "cluster-a", "deployment", "demo", "apps", map[string]string{"env": "prod"}, true)
	if err != nil {
		t.Fatalf("addLabelsInCluster() unexpected error: %v", err)
	}
	if addResult.Status != "would-label" || !strings.Contains(addResult.Message, "Would add labels") {
		t.Fatalf("unexpected dry-run add result: %#v", addResult)
	}

	removeResult, err := s.removeLabelsInCluster(context.Background(), nil, "cluster-a", "deployment", "demo", "apps", []string{"env"}, true)
	if err != nil {
		t.Fatalf("removeLabelsInCluster() unexpected error: %v", err)
	}
	if removeResult.Status != "would-unlabel" || !strings.Contains(removeResult.Message, "Would remove labels") {
		t.Fatalf("unexpected dry-run remove result: %#v", removeResult)
	}

	unsupportedAdd, err := s.addLabelsInCluster(context.Background(), nil, "cluster-a", "widget", "demo", "apps", map[string]string{"env": "prod"}, false)
	if err != nil {
		t.Fatalf("addLabelsInCluster() unexpected error for unsupported kind: %v", err)
	}
	if unsupportedAdd.Status != "failed" || !strings.Contains(unsupportedAdd.Message, "Unsupported resource kind") {
		t.Fatalf("unexpected unsupported add result: %#v", unsupportedAdd)
	}

	unsupportedRemove, err := s.removeLabelsInCluster(context.Background(), nil, "cluster-a", "widget", "demo", "apps", []string{"env"}, false)
	if err != nil {
		t.Fatalf("removeLabelsInCluster() unexpected error for unsupported kind: %v", err)
	}
	if unsupportedRemove.Status != "failed" || !strings.Contains(unsupportedRemove.Message, "Unsupported resource kind") {
		t.Fatalf("unexpected unsupported remove result: %#v", unsupportedRemove)
	}
}

func TestHandleLabelValidation(t *testing.T) {
	s := &Server{}
	tests := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "add invalid json",
			call: func() error {
				_, err := s.handleAddLabels(context.Background(), json.RawMessage("{"))
				return err
			},
			want: "invalid arguments",
		},
		{
			name: "add missing kind",
			call: func() error {
				_, err := s.handleAddLabels(context.Background(), json.RawMessage(`{"name":"demo","labels":{"env":"prod"}}`))
				return err
			},
			want: "kind and name are required",
		},
		{
			name: "add missing labels",
			call: func() error {
				_, err := s.handleAddLabels(context.Background(), json.RawMessage(`{"kind":"pod","name":"demo"}`))
				return err
			},
			want: "labels are required",
		},
		{
			name: "remove invalid json",
			call: func() error {
				_, err := s.handleRemoveLabels(context.Background(), json.RawMessage("{"))
				return err
			},
			want: "invalid arguments",
		},
		{
			name: "remove missing kind",
			call: func() error {
				_, err := s.handleRemoveLabels(context.Background(), json.RawMessage(`{"name":"demo","labels":["env"]}`))
				return err
			},
			want: "kind and name are required",
		},
		{
			name: "remove missing labels",
			call: func() error {
				_, err := s.handleRemoveLabels(context.Background(), json.RawMessage(`{"kind":"pod","name":"demo"}`))
				return err
			},
			want: "labels are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func decodeLabelPatch(t *testing.T, patch []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(patch, &out); err != nil {
		t.Fatalf("failed to decode patch: %v", err)
	}
	return out
}
