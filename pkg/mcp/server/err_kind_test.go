package server

import (
	"context"
	"testing"
	"time"

	"github.com/kubestellar/kubestellar-mcp/pkg/metrics"
	"github.com/stretchr/testify/assert"
)

func TestErrKindFromContextLiveContext(t *testing.T) {
	ctx := context.Background()

	assert.Equal(t, metrics.ErrorKind(""), errKindFromContext(ctx))
}

func TestErrKindFromContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.Equal(t, metrics.ErrorKindTimeout, errKindFromContext(ctx))
}

func TestErrKindFromContextDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	assert.Equal(t, metrics.ErrorKindTimeout, errKindFromContext(ctx))
}

func TestErrKindFromContextNotAnError(t *testing.T) {
	// A context that is neither canceled nor past its deadline yields the
	// empty ErrorKind, which RecordToolCall normalizes to ErrorKindUnknown
	// only when the call actually failed.
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	assert.Equal(t, metrics.ErrorKind(""), errKindFromContext(ctx))
}
