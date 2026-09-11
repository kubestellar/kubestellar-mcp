package gitops

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/rest"
)

// TestNewRESTMapper_SuccessPath covers the previously-uncovered success
// return in newRESTMapper: when discovery.NewDiscoveryClientForConfig and
// restmapper.GetAPIGroupResources both succeed, the function returns the
// wired-up meta.RESTMapper rather than nil. Existing coverage
// (constructors_test.go:TestNewRESTMapper_DegradesGracefully) only
// exercises the two error branches — the `return
// restmapper.NewDiscoveryRESTMapper(gr)` line was 0% before this test.
//
// The strategy is to stand up an httptest.Server that speaks just enough
// of the Kubernetes discovery API to satisfy client-go:
//   - GET /api            → APIVersions with a single "v1" version
//   - GET /apis           → APIGroupList (empty, but valid JSON)
//   - GET /api/v1         → APIResourceList (empty, but valid JSON)
// This is the minimum the discovery client's GetAPIGroupResources needs
// to complete without error.
func TestNewRESTMapper_SuccessPath(t *testing.T) {
	handler := http.NewServeMux()

	// /api → core APIVersions (must list at least one version so that the
	// discovery client will subsequently fetch /api/<v> resources).
	handler.HandleFunc("/api", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"kind":"APIVersions",
			"versions":["v1"],
			"serverAddressByClientCIDRs":[{"clientCIDR":"0.0.0.0/0","serverAddress":""}]
		}`))
	})

	// /api/v1 → APIResourceList for the core group (empty is fine).
	handler.HandleFunc("/api/v1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"kind":"APIResourceList",
			"groupVersion":"v1",
			"resources":[]
		}`))
	})

	// /apis → APIGroupList (no non-core groups; also fine).
	handler.HandleFunc("/apis", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"kind":"APIGroupList",
			"groups":[]
		}`))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	mapper := newRESTMapper(&rest.Config{Host: server.URL})
	if mapper == nil {
		t.Fatal("newRESTMapper returned nil on the success path; expected a wired-up meta.RESTMapper")
	}
}
