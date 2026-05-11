package updater

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withFakeGitHub spins up an httptest server that pretends to be
// both api.github.com and github.com for this repo, then points
// APIBase / ReleaseRedirectBase at it for the duration of the test.
// The handler is the user-supplied test logic.
func withFakeGitHub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	oldAPI, oldRedirect := APIBase, ReleaseRedirectBase
	APIBase = srv.URL
	ReleaseRedirectBase = srv.URL
	t.Cleanup(func() {
		APIBase = oldAPI
		ReleaseRedirectBase = oldRedirect
		srv.Close()
	})
	return srv
}

// ---- Latest via API ------------------------------------------------------

func TestLatest_API_HappyPath(t *testing.T) {
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{
  "tag_name": "v0.3.4",
  "assets": [
    {"name": "rkload_0.3.4_darwin_arm64.tar.gz", "browser_download_url": "https://example.com/d"},
    {"name": "checksums.txt", "browser_download_url": "https://example.com/c"}
  ]
}`)
	})

	rel, err := Latest(nil)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != "v0.3.4" {
		t.Errorf("Tag = %q, want v0.3.4", rel.Tag)
	}
	if rel.Source != "api" {
		t.Errorf("Source = %q, want api", rel.Source)
	}
	if !rel.Verified {
		t.Error("Verified should be true for API-discovered release")
	}
	if rel.Assets["rkload_0.3.4_darwin_arm64.tar.gz"] != "https://example.com/d" {
		t.Errorf("Assets not parsed: %v", rel.Assets)
	}
	if _, ok := rel.Assets["checksums.txt"]; !ok {
		t.Error("checksums.txt missing from assets")
	}
}

func TestLatest_API_FailsOverToRedirect(t *testing.T) {
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		// API path → 403 rate-limited. Redirect path → 302 to /releases/tag/v0.3.4.
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "forbidden", http.StatusForbidden)
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			http.Redirect(w, r, "/RKInnovate/rkload/releases/tag/v0.3.4", http.StatusFound)
		case strings.Contains(r.URL.Path, "/releases/tag/"):
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})

	rel, err := Latest(nil)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != "v0.3.4" {
		t.Errorf("Tag = %q, want v0.3.4", rel.Tag)
	}
	if rel.Source != "redirect" {
		t.Errorf("Source = %q, want redirect", rel.Source)
	}
	if rel.Verified {
		t.Error("Verified should be false for redirect-discovered release")
	}
}

func TestLatest_BothFail(t *testing.T) {
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server exploded", http.StatusInternalServerError)
	})

	_, err := Latest(nil)
	if err == nil {
		t.Fatal("expected error when both API and redirect fail")
	}
	if !strings.Contains(err.Error(), "both discovery paths failed") {
		t.Errorf("error should mention both paths, got: %v", err)
	}
}

func TestLatest_APIReturnsEmptyTag(t *testing.T) {
	// The API call "succeeds" structurally but the tag_name is empty.
	// We should reject and fall back to the redirect (which here also
	// fails, so the whole call errors).
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			fmt.Fprint(w, `{"tag_name": "", "assets": []}`)
		default:
			http.Error(w, "nope", http.StatusInternalServerError)
		}
	})

	_, err := Latest(nil)
	if err == nil {
		t.Fatal("expected error when API returns empty tag")
	}
}

// ---- Newer ---------------------------------------------------------------

func TestNewer_StandardComparisons(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.3.3", "v0.3.4", true},
		{"v0.3.4", "v0.3.4", false},
		{"v0.3.4", "v0.3.3", false},
		{"0.3.3", "v0.3.4", true},    // tolerate missing v prefix
		{"v0.3.3", "0.3.4", true},    // either side
		{"v0.3.10", "v0.3.2", false}, // numeric, not lexical
		{"v0.3.2", "v0.3.10", true},
		{"v1.0.0", "v0.99.0", false}, // major beats minor
	}
	for _, c := range cases {
		got, err := Newer(c.current, c.latest)
		if err != nil {
			t.Errorf("Newer(%q,%q) err = %v", c.current, c.latest, err)
			continue
		}
		if got != c.want {
			t.Errorf("Newer(%q,%q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestNewer_DevAlwaysOlder(t *testing.T) {
	cases := []string{"dev", "unknown", "0.3.4-next", "0.3.4-snapshot", ""}
	for _, current := range cases {
		got, err := Newer(current, "v0.3.4")
		if err != nil {
			t.Errorf("Newer(%q, v0.3.4) err = %v", current, err)
			continue
		}
		if !got {
			t.Errorf("Newer(%q, v0.3.4) = false, want true (dev-like versions are always older)", current)
		}
	}
}

func TestNewer_EmptyLatestNeverNewer(t *testing.T) {
	got, err := Newer("v0.3.3", "")
	if err != nil {
		t.Fatalf("Newer: %v", err)
	}
	if got {
		t.Error("empty latest should not be considered newer than anything")
	}
}

func TestNewer_GarbageInput(t *testing.T) {
	_, err := Newer("banana", "v0.3.4")
	if err == nil {
		t.Error("expected error for non-semver current version")
	}
	_, err = Newer("v0.3.3", "banana")
	if err == nil {
		t.Error("expected error for non-semver latest version")
	}
}

func TestNewer_TrimsWhitespaceAndV(t *testing.T) {
	cases := []struct {
		current, latest string
	}{
		{"  v0.3.3 ", "v0.3.4"},
		{"v0.3.3", "  v0.3.4\n"},
	}
	for _, c := range cases {
		got, err := Newer(c.current, c.latest)
		if err != nil || !got {
			t.Errorf("Newer(%q,%q) = %v, %v; want true,nil", c.current, c.latest, got, err)
		}
	}
}
