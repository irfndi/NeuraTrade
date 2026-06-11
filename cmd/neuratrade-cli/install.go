package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"
)

const defaultInstallBinaryName = "neuratrade"

func cliInstall(cCtx *cli.Context) error {
	return installCurrentCLI(cCtx, cCtx.Bool("force"))
}

func cliUpdate(cCtx *cli.Context) error {
	return installCurrentCLI(cCtx, true)
}

func cliInstallStatus(cCtx *cli.Context) error {
	dst := installDestination(cCtx)
	fmt.Println("NeuraTrade CLI Install Status")
	fmt.Println("==============================")
	fmt.Printf("Install path: %s\n", dst)

	info, err := os.Lstat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Status: not installed")
			fmt.Println("Run: neuratrade install")
			return nil
		}
		return fmt.Errorf("inspect installed CLI: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(dst)
		if err != nil {
			return fmt.Errorf("read install symlink: %w", err)
		}
		fmt.Println("Status: installed as symlink")
		fmt.Printf("Target: %s\n", target)
		return nil
	}

	fmt.Println("Status: installed as regular file")
	fmt.Printf("Mode: %s\n", info.Mode().Perm())
	return nil
}

func installCurrentCLI(cCtx *cli.Context, force bool) error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(src); err == nil {
		src = resolved
	}

	dst := installDestination(cCtx)
	copyMode := cCtx.Bool("copy")
	if err := installCLIExecutable(src, dst, copyMode, force); err != nil {
		return err
	}

	if copyMode {
		fmt.Printf("Installed neuratrade CLI: %s\n", dst)
	} else {
		fmt.Printf("Linked neuratrade CLI: %s -> %s\n", dst, src)
	}
	if !pathContains(filepath.Dir(dst)) {
		fmt.Printf("Warning: %s is not currently in PATH\n", filepath.Dir(dst))
	}
	return nil
}

func installDestination(cCtx *cli.Context) string {
	binDir := strings.TrimSpace(cCtx.String("bin-dir"))
	if binDir == "" {
		binDir = defaultInstallBinDir()
	}
	name := strings.TrimSpace(cCtx.String("name"))
	if name == "" {
		name = defaultInstallBinaryName
	}
	return filepath.Join(binDir, name)
}

func defaultInstallBinDir() string {
	if v := strings.TrimSpace(os.Getenv("NEURATRADE_INSTALL_BIN_DIR")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "/usr/local/bin"
	}
	return filepath.Join(home, ".local", "bin")
}

func installCLIExecutable(src, dst string, copyMode, force bool) error {
	if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
		return fmt.Errorf("source and destination are required")
	}
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve destination path: %w", err)
	}
	if srcAbs == dstAbs {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return fmt.Errorf("create install directory: %w", err)
	}
	if _, err := os.Lstat(dstAbs); err == nil {
		if !force {
			return fmt.Errorf("%s already exists; rerun with --force or use `neuratrade update`", dstAbs)
		}
		if err := os.Remove(dstAbs); err != nil {
			return fmt.Errorf("replace existing install path: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect install path: %w", err)
	}

	if copyMode {
		return copyExecutable(srcAbs, dstAbs)
	}
	if err := os.Symlink(srcAbs, dstAbs); err != nil {
		return fmt.Errorf("create install symlink: %w", err)
	}
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source executable: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create destination executable: %w", err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy executable: %w", err)
	}
	if err := out.Chmod(0o755); err != nil {
		_ = out.Close()
		return fmt.Errorf("set executable mode: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close destination executable: %w", err)
	}
	return nil
}

func pathContains(dir string) bool {
	dir = filepath.Clean(dir)
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(entry) == dir {
			return true
		}
	}
	return false
}

func installFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "bin-dir",
			Usage: "directory where the neuratrade command is installed",
			Value: defaultInstallBinDir(),
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "installed command name",
			Value: defaultInstallBinaryName,
		},
		&cli.BoolFlag{
			Name:  "copy",
			Usage: "copy the current executable instead of creating a symlink",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "replace an existing install path",
		},
	}
}
