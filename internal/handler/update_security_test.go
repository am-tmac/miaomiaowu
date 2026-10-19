package handler

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

type updaterRoundTripper func(*http.Request) (*http.Response, error)

func (f updaterRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func updaterTransport(t *testing.T, f updaterRoundTripper) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
}
func updaterResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestUpdaterRejectsUnverifiedReleases(t *testing.T) {
	for _, digest := range []string{"", "sha256:bad", "sha256:" + strings.Repeat("0", 64)} {
		for _, sse := range []bool{false, true} {
			t.Run(fmt.Sprintf("digest=%s/sse=%t", digest, sse), func(t *testing.T) {
				t.Setenv("UPDATE_GITHUB_REPO", "")
				updaterTransport(t, func(r *http.Request) (*http.Response, error) {
					if r.URL.Host == "api.github.com" {
						return updaterResponse(fmt.Sprintf(`{"tag_name":"v999.0.0","assets":[{"name":%q,"browser_download_url":"https://github.com/am-tmac/miaomiaowu/releases/download/v999.0.0/binary","digest":%q}]}`, "mmw-"+runtime.GOOS+"-"+runtime.GOARCH, digest)), nil
					}
					return updaterResponse("corrupted binary"), nil
				})
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/update", nil)
				if sse {
					NewUpdateApplySSEHandler().ServeHTTP(w, req)
				} else {
					NewUpdateApplyHandler().ServeHTTP(w, req)
				}
				if !strings.Contains(w.Body.String(), "SHA256") {
					t.Fatalf("expected fail-closed SHA256 error before target replacement: status=%d body=%s", w.Code, w.Body)
				}
			})
		}
	}
}

func TestUpdaterVerifiedDownload(t *testing.T) {
	body := "verified executable"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	updaterTransport(t, func(r *http.Request) (*http.Response, error) { return updaterResponse(body), nil })
	name, err := downloadVerifiedBinary("https://github.com/owner/repo/releases/download/v1/mmw", digest, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(name)
	got, err := os.ReadFile(name)
	if err != nil || string(got) != body {
		t.Fatalf("download=%q err=%v", got, err)
	}
}

func TestUpdaterProxyCorruptionIsRejectedAndCleaned(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	calls := 0
	updaterTransport(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("direct download failed")
		}
		if !strings.HasPrefix(r.URL.String(), githubProxyURL) {
			t.Fatalf("expected proxy retry: %s", r.URL)
		}
		return updaterResponse("proxy-corrupted executable"), nil
	})
	retried := false
	name, err := downloadVerifiedBinary("https://github.com/owner/repo/releases/download/v1/mmw", strings.Repeat("0", 64), nil, func(string) { retried = true })
	if err == nil || name != "" || !strings.Contains(err.Error(), "SHA256") || !retried {
		t.Fatalf("name=%q err=%v retried=%t", name, err, retried)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("unverified downloads left behind: %v err=%v", files, err)
	}
}

func TestUpdaterRepositorySelection(t *testing.T) {
	for _, tc := range []struct {
		repo, want string
		invalid    bool
	}{
		{"", "am-tmac/miaomiaowu", false},
		{"another-owner/another.repo", "another-owner/another.repo", false},
		{"https://github.com/owner/repo", "", true},
		{"owner/repo/extra", "", true},
		{"owner/repo?redirect=evil", "", true},
		{"owner/..", "", true},
		{" owner/repo", "", true},
	} {
		t.Run(tc.repo, func(t *testing.T) {
			t.Setenv("UPDATE_GITHUB_REPO", tc.repo)
			calls := 0
			updaterTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.github.com" || r.URL.Path != "/repos/"+tc.want+"/releases/latest" {
					t.Errorf("unexpected API URL: %s", r.URL)
				}
				return updaterResponse(`{"tag_name":"v999.0.0","assets":[]}`), nil
			})
			_, err := checkLatestVersion()
			if tc.invalid {
				if err == nil || calls != 0 {
					t.Fatalf("invalid repository must fail before network: err=%v calls=%d", err, calls)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
