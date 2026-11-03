package notification

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func configEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "8080")
	t.Setenv("MODE", "all")
	t.Setenv("DELIVERY_ENABLED", "false")
	t.Setenv("SEND_INTERVAL", "1s")
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/valto")
	t.Setenv("EVOLUTION_API_URL", "https://evolution.example.invalid")
	t.Setenv("EVOLUTION_API_KEY", strings.Repeat("e", 32))
	t.Setenv("EVOLUTION_INSTANCE_NAME", "Valto")
	t.Setenv("CLIENT_ID", "")
	t.Setenv("VALTO_API_KEY", "")
	// An obsolete file setting must neither be required nor read.
	t.Setenv("CLIENTS_CONFIG_FILE", "/does/not/exist/clients.json")
}

func TestLoadConfigUsesEnvironmentWithoutFile(t *testing.T) {
	configEnvironment(t)
	cfg, err := LoadConfig()
	if err != nil { t.Fatal(err) }
	if len(cfg.Tenants) != 1 { t.Fatal("expected configured client") }
	client := cfg.Tenants[0]
	wantKey := strings.Repeat("e", 32)
	if client.ID != "bunker" || client.APIKey != wantKey || client.TokenHash != sha256.Sum256([]byte(wantKey)) ||
		client.Evolution.URL != "https://evolution.example.invalid" || client.Evolution.APIKey != wantKey || client.Evolution.Instance != "Valto" {
		t.Fatal("environment configuration not applied")
	}
	if cfg.DeliveryEnabled { t.Fatal("delivery unexpectedly enabled") }
}

func TestLoadConfigAllowsOptionalClientOverride(t *testing.T) {
	configEnvironment(t)
	t.Setenv("CLIENT_ID", "another-client")
	t.Setenv("VALTO_API_KEY", strings.Repeat("v", 32))
	cfg, err := LoadConfig()
	if err != nil { t.Fatal(err) }
	if cfg.Tenants[0].ID != "another-client" || cfg.Tenants[0].APIKey != strings.Repeat("v", 32) || cfg.Tenants[0].Evolution.APIKey != strings.Repeat("e", 32) {
		t.Fatal("optional overrides not applied")
	}
}

func TestLoadConfigReportsMissingEvolutionVariableWithoutSecret(t *testing.T) {
	for _, field := range []string{"EVOLUTION_API_URL", "EVOLUTION_API_KEY", "EVOLUTION_INSTANCE_NAME"} {
		t.Run(field, func(t *testing.T) {
			configEnvironment(t)
			t.Setenv(field, "")
			_, err := LoadConfig()
			if err == nil || !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), strings.Repeat("e", 32)) {
				t.Fatal("missing variable error must be explicit and not expose secrets")
			}
		})
	}
}
