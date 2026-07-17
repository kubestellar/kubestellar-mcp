package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestHandleAppNamespaceValidation covers the security-relevant rejection
// branches in the three app handlers where the namespace argument is
// checked by both claude.ValidateK8sNamespace (syntactic RFC 1123) and
// server.ValidateNamespace (blocked-list, see #377). Both wrap the
// underlying error as "invalid namespace" so a single assertion covers
// both paths.
func TestHandleAppNamespaceValidation(t *testing.T) {
	s := &Server{}

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "instances blocked system namespace",
			call: func() error {
				_, err := s.handleGetAppInstances(context.Background(), json.RawMessage(`{"app":"demo","namespace":"kube-system"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "instances openshift-prefixed namespace",
			call: func() error {
				_, err := s.handleGetAppInstances(context.Background(), json.RawMessage(`{"app":"demo","namespace":"openshift-monitoring"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "instances invalid namespace format",
			call: func() error {
				_, err := s.handleGetAppInstances(context.Background(), json.RawMessage(`{"app":"demo","namespace":"Invalid_NS"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "status blocked system namespace",
			call: func() error {
				_, err := s.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo","namespace":"kube-public"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "status gatekeeper-system namespace",
			call: func() error {
				_, err := s.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":"demo","namespace":"gatekeeper-system"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "logs blocked system namespace",
			call: func() error {
				_, err := s.handleGetAppLogs(context.Background(), json.RawMessage(`{"app":"demo","namespace":"kube-node-lease"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "logs openshift-prefixed namespace",
			call: func() error {
				_, err := s.handleGetAppLogs(context.Background(), json.RawMessage(`{"app":"demo","namespace":"openshift-logging"}`))
				return err
			},
			want: "invalid namespace",
		},
		{
			name: "logs invalid namespace format",
			call: func() error {
				_, err := s.handleGetAppLogs(context.Background(), json.RawMessage(`{"app":"demo","namespace":"Invalid_NS"}`))
				return err
			},
			want: "invalid namespace",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// TestHandleAppInvalidAppName covers the validation branch for the App
// argument itself, which every app handler validates first via
// claude.ValidateK8sName. The three handlers wrap the error identically
// as "invalid app name".
func TestHandleAppInvalidAppName(t *testing.T) {
	s := &Server{}

	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "instances empty app",
			call: func() error {
				_, err := s.handleGetAppInstances(context.Background(), json.RawMessage(`{"app":""}`))
				return err
			},
		},
		{
			name: "instances uppercase app",
			call: func() error {
				_, err := s.handleGetAppInstances(context.Background(), json.RawMessage(`{"app":"MyApp"}`))
				return err
			},
		},
		{
			name: "status empty app",
			call: func() error {
				_, err := s.handleGetAppStatus(context.Background(), json.RawMessage(`{"app":""}`))
				return err
			},
		},
		{
			name: "logs empty app",
			call: func() error {
				_, err := s.handleGetAppLogs(context.Background(), json.RawMessage(`{"app":""}`))
				return err
			},
		},
		{
			name: "logs shell injection app",
			call: func() error {
				_, err := s.handleGetAppLogs(context.Background(), json.RawMessage(`{"app":"demo;rm -rf /"}`))
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil || !strings.Contains(err.Error(), "invalid app name") {
				t.Fatalf("error = %v, want substring %q", err, "invalid app name")
			}
		})
	}
}
