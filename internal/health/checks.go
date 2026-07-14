package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// DirWritable returns a readiness Check that dir exists and is actually writable.
//
// It writes and removes a probe file rather than reading permission bits, which
// say nothing about a read-only mount, a full filesystem, or SELinux denying the
// write — all ordinary on-prem failures for the Storage Agent's state dir.
func DirWritable(name, dir string) Check {
	return Check{
		Name: name,
		Func: func(ctx context.Context) error {
			// Up front, not only in the select: `select` picks uniformly at random
			// among ready cases, so an expired ctx + a fast filesystem would be a
			// coin flip.
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("probe %s: %w", dir, err)
			}

			// Filesystem syscalls are UNINTERRUPTIBLE: no context can cancel an
			// os.Stat wedged on a dead NFS/SMB mount, so running it inline would hang
			// the /readyz handler itself. Buffered so the orphaned goroutine can exit.
			done := make(chan error, 1)
			go func() { done <- probeDirWritable(dir) }()

			select {
			case err := <-done:
				return err
			case <-ctx.Done():
				return fmt.Errorf("probe %s: %w", dir, ctx.Err())
			}
		},
	}
}

// probeDirWritable does the actual filesystem work. Blocking by nature.
func probeDirWritable(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: not a directory", dir)
	}

	// CreateTemp: unique name, so concurrent probes cannot collide.
	f, err := os.CreateTemp(dir, ".pivox-health-*")
	if err != nil {
		return fmt.Errorf("write to %s: %w", dir, err)
	}
	path := f.Name()
	// Close before remove: on Windows an open file cannot be unlinked.
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close probe in %s: %w", filepath.Dir(path), err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove probe %s: %w", path, err)
	}
	return nil
}
