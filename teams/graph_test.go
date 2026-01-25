package teams

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGraphCredentialsFromEnvLoadsExpectedKeys(t *testing.T) {
	t.Setenv(EnvTenantID, "preset-tenant")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("AZURE_TENANT_ID=from-file\nAZURE_CLIENT_ID=client\nAZURE_CLIENT_SECRET=secret\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	creds, err := LoadGraphCredentialsFromEnv(path)
	if err != nil {
		t.Fatalf("LoadGraphCredentialsFromEnv: %v", err)
	}

	if got := os.Getenv(EnvTenantID); got != "preset-tenant" {
		t.Fatalf("AZURE_TENANT_ID overridden: %s", got)
	}
	if creds.TenantID != "preset-tenant" {
		t.Fatalf("TenantID mismatch: %s", creds.TenantID)
	}
	if creds.ClientID != "client" {
		t.Fatalf("ClientID mismatch: %s", creds.ClientID)
	}
	if creds.ClientSecret != "secret" {
		t.Fatalf("ClientSecret mismatch: %s", creds.ClientSecret)
	}
}

func TestLoadGraphCredentialsFromEnvMissingKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte("AZURE_CLIENT_ID=client\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if _, err := LoadGraphCredentialsFromEnv(path); err == nil {
		t.Fatalf("expected error for missing env vars")
	}
}
