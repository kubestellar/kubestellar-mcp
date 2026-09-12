package metrics

import (
	"context"
	"encoding/json"
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ClassifyError maps a Go error to a closed ErrorKind using only typed error
// checks (errors.Is / errors.As / apierrors predicates) - never by matching
// on err.Error() text, so classification stays independent of message
// wording and cannot leak raw error content into a metric label.
//
// A nil error classifies as ErrorKindUnknown; callers should only invoke
// this when an error is already known to be present (isError == true).
func ClassifyError(err error) ErrorKind {
	if err == nil {
		return ErrorKindUnknown
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrorKindTimeout
	}

	var statusErr apierrors.APIStatus
	if errors.As(err, &statusErr) {
		if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) {
			return ErrorKindTimeout
		}
		return ErrorKindK8sAPI
	}

	var syntaxErr *json.SyntaxError
	var unmarshalTypeErr *json.UnmarshalTypeError
	var marshalerErr *json.MarshalerError
	if errors.As(err, &syntaxErr) || errors.As(err, &unmarshalTypeErr) || errors.As(err, &marshalerErr) {
		return ErrorKindMarshal
	}

	return ErrorKindUnknown
}
