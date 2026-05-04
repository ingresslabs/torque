package helpui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	ktldocs "github.com/ingresslabs/ktl/docs"
	"github.com/ingresslabs/ktl/internal/version"
	"github.com/spf13/cobra"
)

func TestHelpUIIndex_IncludesVersion(t *testing.T) {
	old := version.Version
	version.Version = "1.2.3"
	t.Cleanup(func() { version.Version = old })

	root := &cobra.Command{Use: "ktl"}
	srv := New(":0", root, logr.Discard())

	rr := httptest.NewRecorder()
	srv.handleIndex(rr, httptest.NewRequest("GET", "http://example/", nil))

	body := rr.Body.String()
	if !strings.Contains(body, "ktl 1.2.3") {
		t.Fatalf("expected HTML to include version, got: %q", body)
	}
	if !strings.Contains(body, `src="assets/ktl-showcase.gif"`) {
		t.Fatalf("expected HTML to include showcase image, got: %q", body)
	}
}

func TestHelpUIShowcaseAsset(t *testing.T) {
	rr := httptest.NewRecorder()
	handleGIFAsset(ktldocs.KTLShowcaseGIF)(rr, httptest.NewRequest("GET", "http://example/assets/ktl-showcase.gif", nil))

	if rr.Code != 200 {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "image/gif" {
		t.Fatalf("expected image/gif content type, got %q", got)
	}
	if body := rr.Body.Bytes(); len(body) < 6 || string(body[:6]) != "GIF89a" {
		t.Fatalf("expected GIF89a body, got %d bytes", len(body))
	}
}
