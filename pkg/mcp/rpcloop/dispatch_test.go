package rpcloop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubestellar/kubestellar-mcp/pkg/mcp/protocol"
)

type ctxKey struct{}

// recordingMethods returns Methods whose handlers reply with a result naming
// the handler that ran, and records the ctx each handler received.
func recordingMethods(got *[]string, gotCtx *[]context.Context) Methods {
	handler := func(name string) Handler {
		return func(ctx context.Context, req *protocol.Request) *protocol.Response {
			*got = append(*got, name)
			*gotCtx = append(*gotCtx, ctx)
			return protocol.NewResult(req.ID, name)
		}
	}
	return Methods{
		Initialize: handler("initialize"),
		ToolsList:  handler("tools/list"),
		ToolsCall:  handler("tools/call"),
	}
}

func TestDispatchRoutesServerSpecificMethods(t *testing.T) {
	for _, method := range []string{"initialize", "tools/list", "tools/call"} {
		t.Run(method, func(t *testing.T) {
			var got []string
			var gotCtx []context.Context
			ctx := context.WithValue(context.Background(), ctxKey{}, method)

			resp := Dispatch(ctx, &protocol.Request{JSONRPC: "2.0", ID: 7, Method: method}, recordingMethods(&got, &gotCtx))

			require.NotNil(t, resp)
			assert.Nil(t, resp.Error)
			assert.Equal(t, method, resp.Result)
			assert.Equal(t, 7, resp.ID)
			assert.Equal(t, []string{method}, got)
			require.Len(t, gotCtx, 1)
			assert.Equal(t, method, gotCtx[0].Value(ctxKey{}), "handler must receive the ctx passed to Dispatch")
		})
	}
}

func TestDispatchLifecycleNotificationsReturnNil(t *testing.T) {
	for _, method := range []string{"initialized", "notifications/initialized"} {
		t.Run(method, func(t *testing.T) {
			var got []string
			var gotCtx []context.Context

			resp := Dispatch(context.Background(), &protocol.Request{JSONRPC: "2.0", Method: method}, recordingMethods(&got, &gotCtx))

			assert.Nil(t, resp)
			assert.Empty(t, got, "no server handler should run for a lifecycle notification")
		})
	}
}

func TestDispatchPingReturnsEmptyResult(t *testing.T) {
	var got []string
	var gotCtx []context.Context

	resp := Dispatch(context.Background(), &protocol.Request{JSONRPC: "2.0", ID: "ping-1", Method: "ping"}, recordingMethods(&got, &gotCtx))

	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)
	assert.Equal(t, "ping-1", resp.ID)
	assert.Equal(t, map[string]interface{}{}, resp.Result)
	assert.Empty(t, got)
}

func TestDispatchUnknownMethodReturnsMethodNotFound(t *testing.T) {
	var got []string
	var gotCtx []context.Context

	resp := Dispatch(context.Background(), &protocol.Request{JSONRPC: "2.0", ID: 3, Method: "bogus/method"}, recordingMethods(&got, &gotCtx))

	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, 3, resp.ID)
	assert.Equal(t, -32601, resp.Error.Code)
	assert.Equal(t, "Method not found: bogus/method", resp.Error.Message)
	assert.Nil(t, resp.Result)
	assert.Empty(t, got)
}
