package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// DirWritable returns a readiness Check that verifies dir exists, is a
// directory, and is actually writable.
//
// It writes and removes a probe file rather than inspecting permission bits.
// Bits lie: they say nothing about a read-only mount, a full filesystem, SELinux
// denying the write, or a container running as a uid that does not own the path
// — all of which are ordinary on-prem failures for the Storage Agent, whose
// state dir (sessions, endpoints, denied patterns) must be writable for the
// agent to be any use. An agent with a read-only state dir boots fine and serves
// nothing, which is precisely the "looks healthy, isn't" failure this package
// exists to eliminate.
func DirWritable(name, dir string) Check {
	return Check{
		Name: name,
		Func: func(ctx context.Context) error {
			// Checked up front, not only in the select below: `select` chooses
			// UNIFORMLY AT RANDOM among ready cases, so with an already-expired
			// context and a fast filesystem both cases are ready and the outcome
			// would be a coin flip. Deciding here makes an expired context
			// deterministic.
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("probe %s: %w", dir, err)
			}

			// The probe runs in a goroutine and we select on ctx, because filesystem
			// syscalls are UNINTERRUPTIBLE: a context deadline cannot cancel an
			// os.Stat blocked in D-state on a wedged NFS/SMB/FUSE mount — precisely
			// the on-prem failure this check exists to catch. Called inline it would
			// hang the /readyz handler itself, and a prober that never gets an answer
			// learns nothing — the exact black hole this package exists to eliminate.
			//
			// Buffered by 1 so the orphaned goroutine can always send and exit rather
			// than leaking on a send nobody receives. If the mount is truly wedged the
			// goroutine stays parked in the syscall — unavoidable — but it holds no
			// lock and blocks nothing else.
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

	// CreateTemp gives a unique name, so concurrent probes (or a second replica
	// sharing the path) cannot collide.
	f, err := os.CreateTemp(dir, ".pivox-health-*")
	if err != nil {
		return fmt.Errorf("write to %s: %w", dir, err)
	}
	path := f.Name()
	// Close before remove: on Windows an open file cannot be unlinked, and leaving
	// probe files behind in a customer's state dir would be its own small bug.
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close probe in %s: %w", filepath.Dir(path), err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove probe %s: %w", path, err)
	}
	return nil
}
