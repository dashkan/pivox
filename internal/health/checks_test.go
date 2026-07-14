package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirWritable(t *testing.T) {
	t.Parallel()

	t.Run("passes for a writable directory", func(t *testing.T) {
		t.Parallel()
		c := DirWritable("state-dir", t.TempDir())

		assert.Equal(t, "state-dir", c.Name)
		assert.NoError(t, c.Func(context.Background()))
	})

	t.Run("fails when the directory does not exist", func(t *testing.T) {
		t.Parallel()
		c := DirWritable("state-dir", filepath.Join(t.TempDir(), "nope"))

		assert.Error(t, c.Func(context.Background()))
	})

	t.Run("fails when the path is a file, not a directory", func(t *testing.T) {
		t.Parallel()
		f := filepath.Join(t.TempDir(), "afile")
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))

		c := DirWritable("state-dir", f)

		assert.ErrorContains(t, c.Func(context.Background()), "not a directory")
	})

	t.Run("fails when the directory is not writable", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits are not enforced")
		}
		dir := filepath.Join(t.TempDir(), "readonly")
		require.NoError(t, os.Mkdir(dir, 0o500)) // r-x: readable, not writable

		c := DirWritable("state-dir", dir)

		// A read-only state dir is exactly the on-prem failure this catches: the
		// agent boots, serves nothing useful, and would otherwise look healthy.
		assert.Error(t, c.Func(context.Background()))
	})

	t.Run("leaves no probe file behind", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		require.NoError(t, DirWritable("d", dir).Func(context.Background()))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries, "the writability probe must clean up after itself")
	})

	t.Run("honours a cancelled context instead of blocking", func(t *testing.T) {
		t.Parallel()
		// Filesystem syscalls are uninterruptible: on a wedged NFS/SMB/FUSE mount
		// os.Stat blocks in D-state and no context can cancel it. If the probe ran
		// inline, /readyz would hang and the prober would learn nothing — the exact
		// black hole this package exists to eliminate. So the probe must return on
		// ctx even when the filesystem never answers.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := DirWritable("state-dir", t.TempDir()).Func(ctx)

		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}
