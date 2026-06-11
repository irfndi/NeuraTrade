package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDoctorReportAcceptsFreshRuntimeAndSecretConfig(t *testing.T) {
	home := t.TempDir()
	runtimeCfg := defaultRuntimeConfig(home)
	require.NoError(t, writeRuntimeConfig(home, runtimeCfg))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.json"), []byte(`{
		"security":{"admin_api_key":"abcdefghijklmnopqrstuvwxyz123456","jwt_secret":"abcdefghijklmnopqrstuvwxyz123456"},
		"ai":{"api_key":"secret"},
		"telegram":{"bot_token":"token"}
	}`), 0o600))

	report := collectDoctorReport(home)

	require.Empty(t, report.Errors)
	require.NotEmpty(t, report.OK)
}

func TestDoctorReportFlagsInvalidRuntimeConfig(t *testing.T) {
	home := t.TempDir()
	runtimeCfg := defaultRuntimeConfig(home)
	runtimeCfg.Server.Port = 70000
	runtimeCfg.AI.MinConfidence = 2
	require.NoError(t, writeRuntimeConfig(home, runtimeCfg))

	report := collectDoctorReport(home)

	require.NotEmpty(t, report.Errors)
	require.Contains(t, report.Errors, "runtime server.port must be between 1 and 65535")
	require.Contains(t, report.Errors, "runtime ai.min_confidence must be between 0 and 1")
}
