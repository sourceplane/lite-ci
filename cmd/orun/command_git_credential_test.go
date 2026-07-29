package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// runHelper drives `orun git-credential <op>` with a credential request on
// stdin, in an env pointed at a stub door.
func runHelper(t *testing.T, op, stdin string, env map[string]string) (stdout, stderr string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	root := &cobra.Command{Use: "orun"}
	registerGitCredentialCommand(root)
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs([]string{"git-credential", op})
	if err := root.Execute(); err != nil {
		t.Fatalf("git-credential %s: %v", op, err)
	}
	return out.String(), errBuf.String()
}

func groundedEnv(t *testing.T, doorURL string) map[string]string {
	t.Helper()
	return map[string]string{
		"ORUN_CLOUD_API":      doorURL,
		"ORUN_ORG_ID":         "org_1",
		"ORUN_SESSION_ID":     "as_1",
		"ORUN_SESSION_TOKEN":  "session-bearer",
		"ORUN_REPO_FULL_NAME": "acme/storefront",
		"XDG_RUNTIME_DIR":     t.TempDir(),
	}
}

func stubDoor(t *testing.T, calls *int32, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(calls, 1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"token":     "ghs_minted",
				"expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

const boundRequest = "protocol=https\nhost=github.com\npath=acme/storefront.git\n\n"

func TestGitCredentialGetMintsForTheBoundRepo(t *testing.T) {
	var calls int32
	door := stubDoor(t, &calls, http.StatusOK)
	out, _ := runHelper(t, "get", boundRequest, groundedEnv(t, door.URL))

	if !strings.Contains(out, "username=x-access-token") {
		t.Errorf("stdout missing the username line:\n%s", out)
	}
	if !strings.Contains(out, "password=ghs_minted") {
		t.Errorf("stdout missing the minted password:\n%s", out)
	}
	if calls != 1 {
		t.Errorf("door called %d times, want 1", calls)
	}
}

func TestGitCredentialGetServesOnlyTheBoundRepo(t *testing.T) {
	// A hostile repo cannot use the session's credential to reach elsewhere:
	// the helper answers for its binding and nothing else. Silence lets git
	// fall through to its own helpers rather than failing on our account.
	for _, tc := range []struct{ name, req string }{
		{"another repo on the same host", "protocol=https\nhost=github.com\npath=evil/exfil.git\n\n"},
		{"another host", "protocol=https\nhost=gitlab.com\npath=acme/storefront.git\n\n"},
		{"plain http", "protocol=http\nhost=github.com\npath=acme/storefront.git\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			door := stubDoor(t, &calls, http.StatusOK)
			out, _ := runHelper(t, "get", tc.req, groundedEnv(t, door.URL))
			if out != "" {
				t.Errorf("helper answered for a foreign request:\n%s", out)
			}
			if calls != 0 {
				t.Errorf("door called %d times for a foreign request, want 0", calls)
			}
		})
	}
}

func TestGitCredentialGetWithoutPathStillServes(t *testing.T) {
	// git omits `path` unless credential.useHttpPath is set; the door is the
	// authority on scope, so a host match is enough to ask.
	var calls int32
	door := stubDoor(t, &calls, http.StatusOK)
	out, _ := runHelper(t, "get", "protocol=https\nhost=github.com\n\n", groundedEnv(t, door.URL))
	if !strings.Contains(out, "password=ghs_minted") {
		t.Errorf("helper refused a path-less request:\n%s", out)
	}
}

func TestGitCredentialGetUsesTheCacheOnTheSecondCall(t *testing.T) {
	var calls int32
	door := stubDoor(t, &calls, http.StatusOK)
	env := groundedEnv(t, door.URL)
	runHelper(t, "get", boundRequest, env)
	out, _ := runHelper(t, "get", boundRequest, env)

	if !strings.Contains(out, "password=ghs_minted") {
		t.Errorf("second call did not answer:\n%s", out)
	}
	if calls != 1 {
		t.Errorf("door called %d times across two git operations, want 1 (cached)", calls)
	}
}

func TestGitCredentialGetRefusalIsSilentOnStdout(t *testing.T) {
	// 403 = the tier was lowered or the session is done. git must see an
	// ordinary auth failure; the reason belongs on stderr, never stdout —
	// stdout IS the credential protocol.
	var calls int32
	door := stubDoor(t, &calls, http.StatusForbidden)
	out, errOut := runHelper(t, "get", boundRequest, groundedEnv(t, door.URL))
	if out != "" {
		t.Errorf("stdout should be empty on refusal, got:\n%s", out)
	}
	if !strings.Contains(errOut, "orun git-credential:") {
		t.Errorf("stderr should explain the refusal, got:\n%s", errOut)
	}
}

func TestGitCredentialGetIsInertOutsideAGroundedSession(t *testing.T) {
	// A developer's laptop running `orun` must never have this helper
	// interfere with their own git credentials.
	out, _ := runHelper(t, "get", boundRequest, map[string]string{
		"ORUN_CLOUD_API":      "",
		"ORUN_ORG_ID":         "",
		"ORUN_SESSION_ID":     "",
		"ORUN_SESSION_TOKEN":  "",
		"ORUN_TOKEN_FILE":     "",
		"ORUN_REPO_FULL_NAME": "",
	})
	if out != "" {
		t.Errorf("helper answered outside a grounded session:\n%s", out)
	}
}

func TestGitCredentialStoreNeverPersists(t *testing.T) {
	env := groundedEnv(t, "https://unused.example")
	out, _ := runHelper(t, "store", "protocol=https\nhost=github.com\nusername=x\npassword=leaked\n\n", env)
	if out != "" {
		t.Errorf("store should answer nothing, got:\n%s", out)
	}
	// Whatever git offered must not have landed in the cache.
	cache := filepath.Join(env["XDG_RUNTIME_DIR"], "orun", "repo-token.json")
	if data, err := readFileIfExists(cache); err == nil && strings.Contains(data, "leaked") {
		t.Error("store persisted a git-supplied credential")
	}
}

func TestGitCredentialEraseClearsTheCache(t *testing.T) {
	var calls int32
	door := stubDoor(t, &calls, http.StatusOK)
	env := groundedEnv(t, door.URL)
	runHelper(t, "get", boundRequest, env)
	runHelper(t, "erase", "protocol=https\nhost=github.com\n\n", env)
	runHelper(t, "get", boundRequest, env)

	if calls != 2 {
		t.Errorf("door called %d times, want 2 (erase invalidated the cache)", calls)
	}
}

func TestServesRequestMatching(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  credentialRequest
		full string
		want bool
	}{
		{"exact", credentialRequest{"https", "github.com", "acme/storefront.git"}, "acme/storefront", true},
		{"no .git suffix", credentialRequest{"https", "github.com", "acme/storefront"}, "acme/storefront", true},
		{"leading slash", credentialRequest{"https", "github.com", "/acme/storefront"}, "acme/storefront", true},
		{"case-insensitive host", credentialRequest{"https", "GitHub.com", ""}, "acme/storefront", true},
		{"prefix is not a match", credentialRequest{"https", "github.com", "acme/storefront-evil"}, "acme/storefront", false},
		{"unbound session", credentialRequest{"https", "github.com", "acme/storefront"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := servesRequest(tc.req, tc.full); got != tc.want {
				t.Errorf("servesRequest(%+v, %q) = %v, want %v", tc.req, tc.full, got, tc.want)
			}
		})
	}
}

func readFileIfExists(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
