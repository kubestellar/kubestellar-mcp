package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// unmarshalableType has no exported fields a json.Marshaler can serialize
// through a channel, so json.Marshal on it returns *json.UnsupportedTypeError.
type unmarshalableType struct {
	C chan int
}

func TestClassifyError(t *testing.T) {
	_, unsupportedTypeErr := json.Marshal(unmarshalableType{C: make(chan int)})
	if unsupportedTypeErr == nil {
		t.Fatal("expected json.Marshal of a channel field to fail")
	}

	cases := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"nil", nil, ErrorKind("")},
		{"deadline exceeded directly", context.DeadlineExceeded, ErrorKindTimeout},
		{"wrapped deadline exceeded", fmt.Errorf("call: %w", context.DeadlineExceeded), ErrorKindTimeout},
		{
			"k8s api status error",
			apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "my-pod"),
			ErrorKindK8sAPI,
		},
		{
			"wrapped k8s api status error",
			fmt.Errorf("get pod: %w", apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "my-pod")),
			ErrorKindK8sAPI,
		},
		{"json unsupported type error", unsupportedTypeErr, ErrorKindMarshal},
		{"wrapped json marshal error", fmt.Errorf("encode: %w", unsupportedTypeErr), ErrorKindMarshal},
		{"unrelated error", errors.New("boom"), ErrorKindUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyError(tc.err); got != tc.want {
				t.Errorf("ClassifyError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
