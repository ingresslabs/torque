package helpui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/ingresslabs/torque/internal/version"
	"github.com/spf13/cobra"
)

func TestHelpUIIndex_IncludesVersion(t *testing.T) {
	old := version.Version
	version.Version = "1.2.3"
	t.Cleanup(func() { version.Version = old })

	root := &cobra.Command{Use: "torque"}
	srv := New(":0", root, logr.Discard())

	rr := httptest.NewRecorder()
	srv.handleIndex(rr, httptest.NewRequest("GET", "http://example/", nil))

	body := rr.Body.String()
	if !strings.Contains(body, "torque 1.2.3") {
		t.Fatalf("expected HTML to include version, got: %q", body)
	}
	disallowedSuffix := "." + "gif"
	if strings.Contains(strings.ToLower(body), disallowedSuffix) {
		t.Fatalf("expected HTML to avoid GIF assets, got: %q", body)
	}
}
