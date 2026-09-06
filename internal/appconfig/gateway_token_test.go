package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGatewayTokenPrecedenceAndEnvironmentReference(t *testing.T) {
	root := t.TempDir()
	dotenv := filepath.Join(root, ".env")
	if err := os.WriteFile(dotenv, []byte("OPENCLAW_GATEWAY_TOKEN=dotenv-token\nCUSTOM_GATEWAY_TOKEN=ref-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "environment-token")
	if token := ResolveGatewayToken(dotenv, root); token != "environment-token" {
		t.Fatal("environment must take precedence")
	}
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	if token := ResolveGatewayToken(dotenv, root); token != "dotenv-token" {
		t.Fatal("dotenv fallback lost")
	}
	if err := os.WriteFile(dotenv, []byte("CUSTOM_GATEWAY_TOKEN=ref-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(`{"gateway":{"auth":{"token":{"source":"env","provider":"default","id":"CUSTOM_GATEWAY_TOKEN"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if token := ResolveGatewayToken(dotenv, root); token != "ref-token" {
		t.Fatal("environment SecretRef not resolved")
	}
	if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(`{"gateway":{"auth":{"token":{"source":"exec","id":"do-not-execute"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if token := ResolveGatewayToken(dotenv, root); token != "" {
		t.Fatal("unsupported SecretRef must not execute or stringify")
	}
}
