package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallCLIExecutableCreatesSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "neuratrade-source")
	dst := filepath.Join(dir, "bin", "neuratrade")
	require.NoError(t, os.WriteFile(src, []byte("binary"), 0o755))

	require.NoError(t, installCLIExecutable(src, dst, false, false))

	target, err := os.Readlink(dst)
	require.NoError(t, err)
	require.Equal(t, src, target)
}

func TestInstallCLIExecutableRequiresForceToReplace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "neuratrade-source")
	dst := filepath.Join(dir, "bin", "neuratrade")
	require.NoError(t, os.WriteFile(src, []byte("binary"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o755))

	require.Error(t, installCLIExecutable(src, dst, false, false))
	require.NoError(t, installCLIExecutable(src, dst, false, true))
	target, err := os.Readlink(dst)
	require.NoError(t, err)
	require.Equal(t, src, target)
}

func TestInstallCLIExecutableCanCopyBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "neuratrade-source")
	dst := filepath.Join(dir, "bin", "neuratrade")
	require.NoError(t, os.WriteFile(src, []byte("binary"), 0o755))

	require.NoError(t, installCLIExecutable(src, dst, true, false))

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, []byte("binary"), data)
	info, err := os.Stat(dst)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}
