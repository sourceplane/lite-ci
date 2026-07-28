//go:build !windows

package cliauth

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// flockExclusive takes an exclusive advisory lock (flock LOCK_EX). It polls in
// non-blocking mode so ctx cancellation/deadline is honored — a plain blocking
// flock cannot be interrupted by ctx.
func flockExclusive(ctx context.Context, f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// flockShared takes a shared advisory lock (flock LOCK_SH), polling like
// flockExclusive so ctx is honored. Readers hold it so they never observe a
// writer's mid-update store state.
func flockShared(ctx context.Context, f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func flockUnlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
