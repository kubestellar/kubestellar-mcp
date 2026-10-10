package testsupport

import (
	"testing"
)

// fatalRecorder implements testing.TB via embedding and records Fatalf calls
// instead of terminating, so the failure branch of MustMarshalJSON can be
// exercised without failing the surrounding test.
type fatalRecorder struct {
	testing.TB
	fatalCalled bool
}

func (f *fatalRecorder) Helper() {}

func (f *fatalRecorder) Fatalf(format string, args ...interface{}) {
	f.fatalCalled = true
}

func TestMustMarshalJSONSuccess(t *testing.T) {
	got := MustMarshalJSON(t, map[string]string{"key": "value"})
	want := `{"key":"value"}`
	if string(got) != want {
		t.Fatalf("MustMarshalJSON = %s, want %s", got, want)
	}
}

func TestMustMarshalJSONFailure(t *testing.T) {
	rec := &fatalRecorder{}
	got := MustMarshalJSON(rec, make(chan int)) // channels cannot marshal
	if !rec.fatalCalled {
		t.Fatal("expected Fatalf to be called for unmarshalable value")
	}
	if got != nil {
		t.Fatalf("expected nil result after marshal failure, got %s", got)
	}
}
