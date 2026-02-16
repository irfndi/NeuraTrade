package main

import (
	"os"
	"testing"
)

func TestParseStartArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected StartOptions
	}{
		{
			name: "default options",
			args: []string{},
			expected: StartOptions{
				Native:      false,
				NoBackend:   false,
				NoCCXT:      false,
				NoTelegram:  false,
				Detach:      false,
				ProjectRoot: getProjectRoot(),
			},
		},
		{
			name: "native mode",
			args: []string{"--native"},
			expected: StartOptions{
				Native:      true,
				NoBackend:   false,
				NoCCXT:      false,
				NoTelegram:  false,
				Detach:      false,
				ProjectRoot: getProjectRoot(),
			},
		},
		{
			name: "skip backend",
			args: []string{"--no-backend"},
			expected: StartOptions{
				Native:      false,
				NoBackend:   true,
				NoCCXT:      false,
				NoTelegram:  false,
				Detach:      false,
				ProjectRoot: getProjectRoot(),
			},
		},
		{
			name: "skip ccxt",
			args: []string{"--no-ccxt"},
			expected: StartOptions{
				Native:      false,
				NoBackend:   false,
				NoCCXT:      true,
				NoTelegram:  false,
				Detach:      false,
				ProjectRoot: getProjectRoot(),
			},
		},
		{
			name: "skip telegram",
			args: []string{"--no-telegram"},
			expected: StartOptions{
				Native:      false,
				NoBackend:   false,
				NoCCXT:      false,
				NoTelegram:  true,
				Detach:      false,
				ProjectRoot: getProjectRoot(),
			},
		},
		{
			name: "detached mode",
			args: []string{"--detach"},
			expected: StartOptions{
				Native:      false,
				NoBackend:   false,
				NoCCXT:      false,
				NoTelegram:  false,
				Detach:      true,
				ProjectRoot: getProjectRoot(),
			},
		},
		{
			name: "short detach flag",
			args: []string{"-d"},
			expected: StartOptions{
				Native:      false,
				NoBackend:   false,
				NoCCXT:      false,
				NoTelegram:  false,
				Detach:      true,
				ProjectRoot: getProjectRoot(),
			},
		},
		{
			name: "multiple flags",
			args: []string{"--native", "--no-backend", "--no-telegram"},
			expected: StartOptions{
				Native:      true,
				NoBackend:   true,
				NoCCXT:      false,
				NoTelegram:  true,
				Detach:      false,
				ProjectRoot: getProjectRoot(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseStartArgs(tt.args)

			if result.Native != tt.expected.Native {
				t.Errorf("Native: got %v, want %v", result.Native, tt.expected.Native)
			}
			if result.NoBackend != tt.expected.NoBackend {
				t.Errorf("NoBackend: got %v, want %v", result.NoBackend, tt.expected.NoBackend)
			}
			if result.NoCCXT != tt.expected.NoCCXT {
				t.Errorf("NoCCXT: got %v, want %v", result.NoCCXT, tt.expected.NoCCXT)
			}
			if result.NoTelegram != tt.expected.NoTelegram {
				t.Errorf("NoTelegram: got %v, want %v", result.NoTelegram, tt.expected.NoTelegram)
			}
			if result.Detach != tt.expected.Detach {
				t.Errorf("Detach: got %v, want %v", result.Detach, tt.expected.Detach)
			}
		})
	}
}

func TestCheckDocker(t *testing.T) {
	result := checkDocker()

	if result {
		t.Log("Docker is available")
	} else {
		t.Log("Docker is not available (this is OK if Docker is not installed)")
	}
}

func TestCheckDockerCompose(t *testing.T) {
	result := checkDockerCompose()

	if result {
		t.Log("Docker Compose is available")
	} else {
		t.Log("Docker Compose is not available (this is OK if Docker is not installed)")
	}
}

func TestCopyFile(t *testing.T) {
	src := t.TempDir() + "/src.txt"
	dst := t.TempDir() + "/dst.txt"

	content := []byte("test content")
	if err := writeFile(src, content); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Errorf("copyFile failed: %v", err)
	}

	result, err := readFile(dst)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(result) != string(content) {
		t.Errorf("Content mismatch: got %s, want %s", string(result), string(content))
	}
}

func writeFile(path string, content []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(content)
	return err
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
