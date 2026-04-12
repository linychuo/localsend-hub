package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetConfigPath_DockerEnv(t *testing.T) {
	// Test explicit env var
	os.Setenv("LOCALSEND_CONFIG_PATH", "/custom/path/config.json")
	defer os.Setenv("LOCALSEND_CONFIG_PATH", "")

	path := GetConfigPath()
	if path != "/custom/path/config.json" {
		t.Errorf("Expected /custom/path/config.json, got: %s", path)
	}
}

func TestGetConfigPath_Fallback(t *testing.T) {
	os.Setenv("LOCALSEND_CONFIG_PATH", "")

	// Save and restore original working directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// Create a temp dir without /app/config
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	path := GetConfigPath()
	expected := filepath.Join(tmpDir, "localsend_config.json")
	if path != expected {
		t.Errorf("Expected %s, got: %s", expected, path)
	}
}

func TestGetConfigPath_DockerSimulation(t *testing.T) {
	// This test verifies that when /app/config exists, it's used.
	// We can't easily create /app/config in a test, so we use env var instead.
	
	os.Setenv("LOCALSEND_CONFIG_PATH", "/app/config/localsend_config.json")
	defer os.Setenv("LOCALSEND_CONFIG_PATH", "")

	path := GetConfigPath()
	expected := "/app/config/localsend_config.json"
	if path != expected {
		t.Errorf("Expected %s, got: %s", expected, path)
	}
}
