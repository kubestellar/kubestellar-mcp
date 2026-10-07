package openshift

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func clusterVersionWithConditions(conditions []interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"conditions": conditions,
			},
		},
	}
}

func TestFindProgressingCondition(t *testing.T) {
	tests := []struct {
		name            string
		conditions      []interface{}
		wantProgressing bool
		wantMessage     string
	}{
		{
			name: "progressing true returns message",
			conditions: []interface{}{
				map[string]interface{}{
					"type":    "Progressing",
					"status":  "True",
					"message": "Working towards 4.18.30",
				},
			},
			wantProgressing: true,
			wantMessage:     "Working towards 4.18.30",
		},
		{
			name: "progressing false still returns message",
			conditions: []interface{}{
				map[string]interface{}{
					"type":    "Progressing",
					"status":  "False",
					"message": "Cluster version is 4.18.30",
				},
			},
			wantProgressing: false,
			wantMessage:     "Cluster version is 4.18.30",
		},
		{
			name: "no progressing condition",
			conditions: []interface{}{
				map[string]interface{}{
					"type":   "Available",
					"status": "True",
				},
			},
			wantProgressing: false,
			wantMessage:     "",
		},
		{
			name:            "no conditions",
			conditions:      nil,
			wantProgressing: false,
			wantMessage:     "",
		},
		{
			name: "malformed condition entry is skipped",
			conditions: []interface{}{
				"not-a-map",
				map[string]interface{}{
					"type":    "Progressing",
					"status":  "True",
					"message": "still found it",
				},
			},
			wantProgressing: true,
			wantMessage:     "still found it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv := clusterVersionWithConditions(tt.conditions)
			gotProgressing, gotMessage := FindProgressingCondition(cv)
			if gotProgressing != tt.wantProgressing {
				t.Errorf("progressing = %v, want %v", gotProgressing, tt.wantProgressing)
			}
			if gotMessage != tt.wantMessage {
				t.Errorf("message = %q, want %q", gotMessage, tt.wantMessage)
			}
		})
	}
}
