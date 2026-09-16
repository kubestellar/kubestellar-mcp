package upgrade

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/kubestellar/kubestellar-mcp/pkg/progress"
)

type recordingRenderer struct {
	mu       sync.Mutex
	statuses []progress.Status
}

func (r *recordingRenderer) Render(status progress.Status) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, status)
	return true
}

func (r *recordingRenderer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.statuses)
}

func TestRunWatchLoopWithTicker_CompletesOnFinishedStatus(t *testing.T) {
	dynClient := newFakeDynamicClient(newClusterVersion("4.18.30", "Cluster version is 4.18.30"))
	renderer := &recordingRenderer{}
	ticks := make(chan time.Time)
	var out bytes.Buffer
	var errOut bytes.Buffer

	err := runWatchLoopWithTicker(context.Background(), dynClient, ticks, &out, &errOut, renderer)

	require.NoError(t, err)
	assert.Equal(t, 1, renderer.count())
	assert.Empty(t, errOut.String())
}

func TestRunWatchLoopWithTicker_RendersOnProgressChange(t *testing.T) {
	dynClient := newFakeDynamicClient()
	fakeClient, ok := dynClient.(*dynamicfake.FakeDynamicClient)
	require.True(t, ok)

	statuses := []*unstructured.Unstructured{
		newClusterVersion("4.18.30", "Working towards 4.18.30: 25 of 100 done (25% complete), waiting on one"),
		newClusterVersion("4.18.30", "Working towards 4.18.30: 50 of 100 done (50% complete), waiting on two"),
		newClusterVersion("4.18.30", "Cluster version is 4.18.30"),
	}
	var callCount int32
	fakeClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		idx := int(atomic.AddInt32(&callCount, 1)) - 1
		if idx >= len(statuses) {
			return true, statuses[len(statuses)-1], nil
		}
		return true, statuses[idx], nil
	})

	renderer := &recordingRenderer{}
	ticks := make(chan time.Time, 2)
	ticks <- time.Now()
	ticks <- time.Now()
	var out bytes.Buffer
	var errOut bytes.Buffer

	err := runWatchLoopWithTicker(context.Background(), dynClient, ticks, &out, &errOut, renderer)

	require.NoError(t, err)
	assert.Equal(t, 3, renderer.count())
	assert.Empty(t, errOut.String())
}

func TestRunWatchLoopWithTicker_DoesNotRenderWhenPercentUnchanged(t *testing.T) {
	dynClient := newFakeDynamicClient()
	fakeClient, ok := dynClient.(*dynamicfake.FakeDynamicClient)
	require.True(t, ok)

	statuses := []*unstructured.Unstructured{
		newClusterVersion("4.18.30", "Working towards 4.18.30: 25 of 100 done (25% complete), waiting on one"),
		newClusterVersion("4.18.30", "Working towards 4.18.30: 25 of 100 done (25% complete), waiting on one"),
	}
	var callCount int32
	ctx, cancel := context.WithCancel(context.Background())
	fakeClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		idx := int(atomic.AddInt32(&callCount, 1)) - 1
		if idx >= len(statuses)-1 {
			cancel()
			return true, statuses[len(statuses)-1], nil
		}
		return true, statuses[idx], nil
	})

	renderer := &recordingRenderer{}
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	var out bytes.Buffer
	var errOut bytes.Buffer

	err := runWatchLoopWithTicker(ctx, dynClient, ticks, &out, &errOut, renderer)

	require.NoError(t, err)
	assert.Equal(t, 1, renderer.count())
	assert.Contains(t, out.String(), "Stopped watching.")
}

func TestRunWatchLoopWithTicker_WritesErrorAndContinues(t *testing.T) {
	dynClient := newFakeDynamicClient()
	fakeClient, ok := dynClient.(*dynamicfake.FakeDynamicClient)
	require.True(t, ok)

	var callCount int32
	fakeClient.PrependReactor("get", "clusterversions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		idx := atomic.AddInt32(&callCount, 1)
		if idx == 1 {
			return true, nil, assert.AnError
		}
		return true, newClusterVersion("4.18.30", "Cluster version is 4.18.30"), nil
	})

	renderer := &recordingRenderer{}
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	var out bytes.Buffer
	var errOut bytes.Buffer

	err := runWatchLoopWithTicker(context.Background(), dynClient, ticks, &out, &errOut, renderer)

	require.NoError(t, err)
	assert.Contains(t, errOut.String(), "Error fetching status")
	assert.Equal(t, 1, renderer.count())
}

func TestRunWatchLoopWithTicker_CanceledContextStopsCleanly(t *testing.T) {
	dynClient := newFakeDynamicClient(newClusterVersion("4.18.30", "Working towards 4.18.30: 10 of 100 done (10% complete), waiting on one"))
	renderer := &recordingRenderer{}
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	var out bytes.Buffer
	var errOut bytes.Buffer

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := runWatchLoopWithTicker(ctx, dynClient, ticks, &out, &errOut, renderer)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "Stopped watching.")
	assert.Equal(t, 1, renderer.count())
	assert.Empty(t, errOut.String())
}
