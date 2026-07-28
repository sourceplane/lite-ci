package cliauth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// storeLockName is the advisory lock that guards the credential STORE itself
// (the keychain entry and ~/.orun/credentials.json), as distinct from
// refresh.lock which serializes the refresh REDEMPTION critical section.
//
// Why a second lock: the store update is not atomic. The macOS keychain write
// (`security add-generic-password -U`) has a delete/re-add window in which a
// concurrent read finds nothing, and the keychain→file fallback can leave the
// two backends disagreeing. An unguarded reader in that window sees "not
// logged in" (workflow steps flapping between runs), and — worse — a reader
// that picks up the STALE backend later redeems an already-spent rotating
// refresh token, trips the platform's reuse-detection, and kills the whole
// token family. Readers take the lock shared, writers exclusive, so a load
// never interleaves with a save/clear.
//
// Lock ordering: refresh.lock is always acquired BEFORE store.lock (the
// refresh critical section loads and saves under it); nothing acquires them
// in the other order, so the pair cannot deadlock.
const storeLockName = "store.lock"

// storeLockTimeout bounds how long a store operation waits for the lock. Store
// operations are short (one file or one `security` invocation); a holder
// stuck longer than this is presumed dead and the operation proceeds
// unlocked — best-effort, matching the refresh lock's philosophy.
const storeLockTimeout = 10 * time.Second

// withStoreLock runs fn under the cross-process store lock (shared for reads,
// exclusive for writes). Locking is best-effort: when the lock cannot be
// opened or acquired in time, fn runs unlocked — an unserialized store access
// is still correct, just no longer race-free.
func withStoreLock(exclusive bool, fn func() error) error {
	release, err := acquireStoreLock(exclusive)
	if err == nil {
		defer release()
	}
	return fn()
}

func acquireStoreLock(exclusive bool) (func(), error) {
	base, err := ensureConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(base, storeLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open store lock %s: %w", path, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeLockTimeout)
	defer cancel()
	if exclusive {
		err = flockExclusive(ctx, f)
	} else {
		err = flockShared(ctx, f)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = flockUnlock(f)
		_ = f.Close()
	}, nil
}
