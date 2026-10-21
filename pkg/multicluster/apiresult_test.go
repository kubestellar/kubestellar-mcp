package multicluster

import (
	"errors"
	"testing"
)

func TestClassifyMutationResult(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		successStatus string
		notFoundMsg   string
		wantStatus    string
		wantMessage   string
	}{
		{
			name:          "nil error returns success status with empty message",
			err:           nil,
			successStatus: "deleted",
			notFoundMsg:   "resource not found in cluster",
			wantStatus:    "deleted",
			wantMessage:   "",
		},
		{
			name:          "not found error returns not-found with provided message",
			err:           errors.New(`pods "web" not found`),
			successStatus: "labeled",
			notFoundMsg:   "resource not found in cluster",
			wantStatus:    "not-found",
			wantMessage:   "resource not found in cluster",
		},
		{
			name:          "other error returns failed with error text",
			err:           errors.New("connection refused"),
			successStatus: "updated",
			notFoundMsg:   "resource not found in cluster",
			wantStatus:    "failed",
			wantMessage:   "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, message := ClassifyMutationResult(tt.err, tt.successStatus, tt.notFoundMsg)
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
			if message != tt.wantMessage {
				t.Errorf("message = %q, want %q", message, tt.wantMessage)
			}
		})
	}
}
