package main

import (
	"testing"
)

func TestParseStopArgs(t *testing.T) {
	result := parseStopArgs([]string{})

	if result.ProjectRoot == "" {
		t.Error("ProjectRoot should not be empty")
	}
}

func TestIsDockerRunning(t *testing.T) {
	result := isDockerRunning()

	if result {
		t.Log("Docker containers are running")
	} else {
		t.Log("No Docker containers running (this is OK)")
	}
}
