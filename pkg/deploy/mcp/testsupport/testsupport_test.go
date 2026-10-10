package testsupport

import (
	"encoding/json"
	"testing"
)

func TestMustMarshalJSON(t *testing.T) {
	got := MustMarshalJSON(t, map[string]string{"a": "b"})

	var decoded map[string]string
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded["a"] != "b" {
		t.Fatalf("got %v, want map with a=b", decoded)
	}
}
