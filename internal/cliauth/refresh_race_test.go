package cliauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// rotatingTokenBackend is a fake platform token endpoint with the property
// that makes concurrent refresh dangerous in production: the refresh token is
// rotating and SINGLE-USE, and presenting a spent one is reuse — the whole
// family is revoked and every later call fails 410. Concurrency-safe so
// racing clients exercise it exactly like the real backend.
type rotatingTokenBackend struct {
	t *testing.T

	mu           sync.Mutex
	current      string // the one outstanding (unspent) refresh token
	redemptions  int
	reuses       int
	revoked      bool
	accessExpiry func(n int) time.Time // expiry for the nth issued access token
}

func (b *rotatingTokenBackend) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/cli/token" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			GrantType    string `json:"grantType"`
			RefreshToken string `json:"refreshToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.GrantType != "refresh_token" {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}

		b.mu.Lock()
		defer b.mu.Unlock()
		if b.revoked || body.RefreshToken != b.current {
			// Single-use reuse (or a call after the family died): revoke the
			// family, exactly like the platform's reuse-detection.
			b.reuses++
			b.revoked = true
			w.WriteHeader(http.StatusGone)
			fmt.Fprint(w, `{"error":"refresh token reused","code":"expired"}`)
			return
		}
		b.redemptions++
		n := b.redemptions
		b.current = fmt.Sprintf("rt-%d", n)
		resp := map[string]any{
			"data": map[string]any{
				"accessToken":  fmt.Sprintf("access-%d", n),
				"expiresAt":    b.accessExpiry(n).Format(time.RFC3339),
				"refreshToken": b.current,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func (b *rotatingTokenBackend) stats() (redemptions, reuses int, revoked bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.redemptions, b.reuses, b.revoked
}

// useTempFileStore points the credential store at a temp HOME with the
// keychain disabled, so the race runs against the real file store + locks.
func useTempFileStore(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	userHomeDir = func() (string, error) { return tmp, nil }
	lookPath = func(file string) (string, error) { return "", errors.New("missing") }
	t.Cleanup(func() {
		userHomeDir = os.UserHomeDir
		lookPath = exec.LookPath
	})
}

func seedSession(t *testing.T, backendURL string) {
	t.Helper()
	if err := SaveSession(&Credentials{
		AccessToken:       "access-0",
		AccessTokenExpiry: time.Now().Add(-time.Minute).Format(time.RFC3339), // already expired
		RefreshToken:      "rt-0",
		BackendURL:        backendURL,
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// TestRefreshSessionLockedConcurrentSingleRedemption: N concurrent refreshers,
// backend issues long-lived access tokens. Exactly ONE may redeem; the rest
// must re-read under the lock, see the winner's fresh access token, and skip.
// Before the lock + re-read existed, each loser replayed the seed's spent
// rt-0, the fake backend's reuse-detection revoked the family, and the
// session died — the "logged in, then suddenly not" flap seen when workflow
// steps and an operator probe ran orun concurrently.
func TestRefreshSessionLockedConcurrentSingleRedemption(t *testing.T) {
	useTempFileStore(t)
	backend := &rotatingTokenBackend{
		t:            t,
		current:      "rt-0",
		accessExpiry: func(int) time.Time { return time.Now().Add(15 * time.Minute) },
	}
	srv := httptest.NewServer(backend.handler())
	defer srv.Close()
	seedSession(t, srv.URL)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	tokens := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			creds, err := RefreshSessionLocked(context.Background(), srv.URL, "test", time.Minute)
			if err != nil {
				errs[i] = err
				return
			}
			tokens[i] = creds.AccessToken
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("refresher %d failed: %v", i, err)
		}
	}
	for i, tok := range tokens {
		if tok != "access-1" {
			t.Fatalf("refresher %d got access token %q, want the single winner's access-1", i, tok)
		}
	}
	redemptions, reuses, revoked := backend.stats()
	if redemptions != 1 {
		t.Fatalf("backend saw %d redemptions, want exactly 1 (losers must skip after re-read)", redemptions)
	}
	if reuses != 0 || revoked {
		t.Fatalf("backend detected token reuse (reuses=%d revoked=%v) — the refresh critical section is not serialized", reuses, revoked)
	}
	final, err := LoadSession()
	if err != nil || final == nil {
		t.Fatalf("final session load: creds=%v err=%v", final, err)
	}
	if final.RefreshToken != "rt-1" {
		t.Fatalf("persisted refresh token = %q, want the rotated rt-1", final.RefreshToken)
	}
}

// TestRefreshSessionLockedConcurrentChain: the backend issues ALREADY-EXPIRED
// access tokens, so every contender must perform a real redemption. The lock
// must serialize them into a clean rotation chain rt-0→rt-1→…→rt-N with zero
// reuse; any interleaving redeems a spent token and kills the family.
func TestRefreshSessionLockedConcurrentChain(t *testing.T) {
	useTempFileStore(t)
	backend := &rotatingTokenBackend{
		t:            t,
		current:      "rt-0",
		accessExpiry: func(int) time.Time { return time.Now().Add(-time.Minute) },
	}
	srv := httptest.NewServer(backend.handler())
	defer srv.Close()
	seedSession(t, srv.URL)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = RefreshSessionLocked(context.Background(), srv.URL, "test", time.Minute)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("refresher %d failed: %v (a serialized chain must keep every contender authenticated)", i, err)
		}
	}
	redemptions, reuses, revoked := backend.stats()
	if reuses != 0 || revoked {
		t.Fatalf("backend detected token reuse (reuses=%d revoked=%v)", reuses, revoked)
	}
	if redemptions != n {
		t.Fatalf("backend saw %d redemptions, want %d (every contender redeems, serially)", redemptions, n)
	}
	final, err := LoadSession()
	if err != nil || final == nil {
		t.Fatalf("final session load: creds=%v err=%v", final, err)
	}
	if want := fmt.Sprintf("rt-%d", n); final.RefreshToken != want {
		t.Fatalf("persisted refresh token = %q, want %q (the end of the rotation chain)", final.RefreshToken, want)
	}
}

// TestConcurrentLoadDuringSaveNeverSeesTornState: hammer LoadSession while a
// writer rewrites the credentials file. Readers must always see a COMPLETE
// session (some version), never a truncated file or a vanished one — the
// torn/missing read is what workflow preflights observed as spurious
// "not logged in" between two successful runs.
func TestConcurrentLoadDuringSaveNeverSeesTornState(t *testing.T) {
	useTempFileStore(t)
	seed := &Credentials{
		AccessToken:       "access-seed",
		AccessTokenExpiry: time.Now().Add(15 * time.Minute).Format(time.RFC3339),
		RefreshToken:      "rt-seed",
		BackendURL:        "https://api.example.com",
	}
	if err := SaveSession(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stop := make(chan struct{})
	var writerWg sync.WaitGroup
	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			creds := *seed
			creds.RefreshToken = fmt.Sprintf("rt-write-%d", i)
			if err := SaveSession(&creds); err != nil {
				t.Errorf("writer save: %v", err)
				return
			}
		}
	}()

	var readerWg sync.WaitGroup
	for r := 0; r < 4; r++ {
		readerWg.Add(1)
		go func() {
			defer readerWg.Done()
			for i := 0; i < 200; i++ {
				creds, err := LoadSession()
				if err != nil {
					t.Errorf("reader: LoadSession error = %v (torn/missing state observed)", err)
					return
				}
				if creds == nil || creds.RefreshToken == "" || creds.AccessToken == "" {
					t.Errorf("reader: incomplete session %+v", creds)
					return
				}
			}
		}()
	}
	readerWg.Wait()
	close(stop)
	writerWg.Wait()
}
