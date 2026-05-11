// Package updater handles self-update for the rkload binary.
//
// Discovery uses the GitHub Releases API as the primary source and
// falls back to parsing the /releases/latest redirect when the API is
// rate-limited or otherwise unreachable. Both paths produce the same
// Release value so callers don't need to know which one succeeded.
//
// Everything in this package is testable against an httptest server
// by overriding APIBase and ReleaseRedirectBase.
package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Repo identifies the GitHub repository to check. Exposed for tests
// (which may want to point at a fake) but not flag-overridable —
// users who want a different fork should fork the build.
var Repo = "RKInnovate/rkload"

// APIBase and ReleaseRedirectBase are the two hosts queried during
// discovery. Both are variables so a test server can replace them
// (set them back in a t.Cleanup).
var (
	APIBase             = "https://api.github.com"
	ReleaseRedirectBase = "https://github.com"
)

// HTTPTimeout caps the total time a discovery call can take. Tighter
// than http.DefaultClient's zero timeout so a hung server doesn't
// block the load test that triggered the background check.
var HTTPTimeout = 5 * time.Second

// Release is the bit of release metadata that matters to callers.
// Empty Tag means "no release found"; non-empty Tag is canonical
// (with the leading 'v', matching GitHub's tag_name).
type Release struct {
	Tag      string            // e.g. "v0.3.4"
	Assets   map[string]string // asset filename → browser download URL
	Source   string            // "api" or "redirect" — for diagnostics
	Verified bool              // true when discovered via API (Assets populated); false for redirect-only
}

// Latest discovers the most recent published release. Tries the API
// first (which gives us full asset URLs and checksums); if that fails
// for any reason — rate limiting, DNS, 5xx — it parses the redirect
// target of /releases/latest, which always works for public repos
// and returns only the tag.
//
// The client is injected so tests can supply an httptest.Client and
// callers can configure retries or proxies.
func Latest(client *http.Client) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: HTTPTimeout}
	}

	rel, apiErr := latestViaAPI(client)
	if apiErr == nil {
		rel.Source = "api"
		rel.Verified = true
		return rel, nil
	}

	rel, redirectErr := latestViaRedirect(client)
	if redirectErr == nil {
		rel.Source = "redirect"
		rel.Verified = false
		return rel, nil
	}

	return Release{}, fmt.Errorf("updater: both discovery paths failed (api: %v; redirect: %v)", apiErr, redirectErr)
}

// latestViaAPI hits api.github.com/repos/<repo>/releases/latest and
// extracts the tag plus the asset name→URL map.
func latestViaAPI(client *http.Client) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", APIBase, Repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "rkload-updater")

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden && strings.Contains(resp.Header.Get("X-RateLimit-Remaining"), "0") {
		return Release{}, errors.New("github API rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("decoding github API response: %w", err)
	}
	if payload.TagName == "" {
		return Release{}, errors.New("github API response had empty tag_name")
	}
	assets := make(map[string]string, len(payload.Assets))
	for _, a := range payload.Assets {
		assets[a.Name] = a.URL
	}
	return Release{Tag: payload.TagName, Assets: assets}, nil
}

// latestViaRedirect follows the /releases/latest redirect and pulls
// the tag out of the resulting URL. Works for public repos without
// auth and bypasses the API rate limit entirely.
func latestViaRedirect(client *http.Client) (Release, error) {
	url := fmt.Sprintf("%s/%s/releases/latest", ReleaseRedirectBase, Repo)
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", "rkload-updater")

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	// We expect Go's default redirect follower to land us on the
	// concrete /releases/tag/<tag> URL.
	finalURL := resp.Request.URL.String()
	idx := strings.LastIndex(finalURL, "/tag/")
	if idx < 0 {
		return Release{}, fmt.Errorf("redirect target %q does not contain /tag/", finalURL)
	}
	tag := finalURL[idx+len("/tag/"):]
	if tag == "" {
		return Release{}, errors.New("empty tag in redirect target")
	}
	return Release{Tag: tag}, nil
}

// Newer reports whether latest is a higher version than current.
//
// "dev" / "snapshot" / "unknown" current versions are treated as
// always older than any released tag, so locally-built binaries
// see update notices. Empty latest is always reported as not newer.
func Newer(current, latest string) (bool, error) {
	if latest == "" {
		return false, nil
	}
	if current == "" || current == "dev" || current == "unknown" || strings.HasSuffix(current, "-next") || strings.HasSuffix(current, "-snapshot") {
		return true, nil
	}
	cur, err := parseSemver(current)
	if err != nil {
		return false, fmt.Errorf("current version %q: %w", current, err)
	}
	lat, err := parseSemver(latest)
	if err != nil {
		return false, fmt.Errorf("latest version %q: %w", latest, err)
	}
	return cur.less(lat), nil
}

// semver is a minimal struct for major.minor.patch comparison. We
// deliberately don't parse prerelease/build metadata — the rkload
// release stream is plain semver, and importing a full semver
// library to handle edge cases we don't produce would be overkill.
type semver struct {
	major, minor, patch int
}

func (a semver) less(b semver) bool {
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	return a.patch < b.patch
}

func parseSemver(s string) (semver, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.SplitN(s, "-", 2) // ignore any "-rc1" suffix for ordering
	parts = strings.SplitN(parts[0], ".", 3)
	if len(parts) < 3 {
		return semver{}, fmt.Errorf("expected major.minor.patch, got %d components", len(parts))
	}
	out := semver{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return semver{}, fmt.Errorf("non-numeric component %q at position %d", p, i)
		}
		switch i {
		case 0:
			out.major = n
		case 1:
			out.minor = n
		case 2:
			out.patch = n
		}
	}
	return out, nil
}
