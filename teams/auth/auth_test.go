package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvLoadsExpectedKeys(t *testing.T) {
	t.Setenv("AZURE_TENANT_ID", "preset-tenant")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("AZURE_TENANT_ID=from-file\nAZURE_CLIENT_ID=client\nAZURE_CLIENT_SECRET=secret\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	if got := os.Getenv("AZURE_TENANT_ID"); got != "preset-tenant" {
		t.Fatalf("AZURE_TENANT_ID overridden: %s", got)
	}
	if got := os.Getenv("AZURE_CLIENT_ID"); got != "client" {
		t.Fatalf("AZURE_CLIENT_ID not set: %s", got)
	}
	if got := os.Getenv("AZURE_CLIENT_SECRET"); got != "secret" {
		t.Fatalf("AZURE_CLIENT_SECRET not set: %s", got)
	}
}
