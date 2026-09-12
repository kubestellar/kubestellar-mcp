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

func TestClassifyErrorNil(t *testing.T) {
	if got := ClassifyError(nil); got != ErrorKindUnknown {
		t.Errorf("ClassifyError(nil) = %q, want %q", got, ErrorKindUnknown)
	}
}

func TestClassifyErrorTimeout(t *testing.T) {
	cases := []error{
		context.DeadlineExceeded,
		context.Canceled,
		fmt.Errorf("dial: %w", context.DeadlineExceeded),
	}
	for _, err := range cases {
		if got := ClassifyError(err); got != ErrorKindTimeout {
			t.Errorf("ClassifyError(%v) = %q, want %q", err, got, ErrorKindTimeout)
		}
	}
}

func TestClassifyErrorK8sAPI(t *testing.T) {
	gvr := schema.GroupResource{Group: "apps", Resource: "deployments"}
	notFound := apierrors.NewNotFound(gvr, "my-app")
	if got := ClassifyError(notFound); got != ErrorKindK8sAPI {
		t.Errorf("ClassifyError(NotFound) = %q, want %q", got, ErrorKindK8sAPI)
	}

	wrapped := fmt.Errorf("get deployment: %w", notFound)
	if got := ClassifyError(wrapped); got != ErrorKindK8sAPI {
		t.Errorf("ClassifyError(wrapped NotFound) = %q, want %q", got, ErrorKindK8sAPI)
	}
}

func TestClassifyErrorK8sTimeout(t *testing.T) {
	gvr := schema.GroupResource{Group: "apps", Resource: "deployments"}
	timeoutErr := apierrors.NewTimeoutError("timed out waiting for condition", 0)
	_ = gvr
	if got := ClassifyError(timeoutErr); got != ErrorKindTimeout {
		t.Errorf("ClassifyError(apierrors timeout) = %q, want %q", got, ErrorKindTimeout)
	}
}

func TestClassifyErrorMarshal(t *testing.T) {
	var target int
	err := json.Unmarshal([]byte(`"not-an-int"`), &target)
	if err == nil {
		t.Fatal("expected json.Unmarshal to fail")
	}
	if got := ClassifyError(err); got != ErrorKindMarshal {
		t.Errorf("ClassifyError(%v) = %q, want %q", err, got, ErrorKindMarshal)
	}
}

func TestClassifyErrorUnknown(t *testing.T) {
	if got := ClassifyError(errors.New("some opaque failure")); got != ErrorKindUnknown {
		t.Errorf("ClassifyError(opaque) = %q, want %q", got, ErrorKindUnknown)
	}
}
