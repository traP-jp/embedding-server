package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsEmptyAPIKeys(t *testing.T) {
	setRequiredConfig(t)
	t.Setenv("API_KEY", "")
	t.Setenv("INTERNAL_API_KEY", "internal-secret")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "API_KEY must not be empty") {
		t.Fatalf("expected empty API_KEY error, got %v", err)
	}

	t.Setenv("API_KEY", "external-secret")
	t.Setenv("INTERNAL_API_KEY", "")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "INTERNAL_API_KEY must not be empty") {
		t.Fatalf("expected empty INTERNAL_API_KEY error, got %v", err)
	}
}

func TestLoadRejectsDisabledAuthInProduction(t *testing.T) {
	setRequiredConfig(t)
	t.Setenv("AUTH_DISABLED", "true")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "must not be true in production") {
		t.Fatalf("expected production auth disable error, got %v", err)
	}
}

func TestLoadAllowsExplicitlyDisabledAuthOutsideProduction(t *testing.T) {
	setRequiredConfig(t)
	t.Setenv("APP_ENV", "debug")
	t.Setenv("AUTH_DISABLED", "true")
	t.Setenv("API_KEY", "")
	t.Setenv("INTERNAL_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.AuthDisabled {
		t.Fatal("expected authentication to be disabled")
	}
}

func TestLoadTrimsAPIKeys(t *testing.T) {
	setRequiredConfig(t)
	t.Setenv("API_KEY", "  external-secret  ")
	t.Setenv("INTERNAL_API_KEY", "  internal-secret  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIKey != "external-secret" || cfg.InternalAPIKey != "internal-secret" {
		t.Fatalf("unexpected keys: external=%q internal=%q", cfg.APIKey, cfg.InternalAPIKey)
	}
}

func TestLoadRejectsSharedExternalAndInternalKey(t *testing.T) {
	setRequiredConfig(t)
	t.Setenv("API_KEY", "same-secret")
	t.Setenv("INTERNAL_API_KEY", "same-secret")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "must be different") {
		t.Fatalf("expected shared key error, got %v", err)
	}
}

func setRequiredConfig(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"APP_ENV":              "production",
		"API_PORT":             "8080",
		"AUTH_DISABLED":        "false",
		"API_KEY":              "external-secret",
		"INTERNAL_API_KEY":     "internal-secret",
		"POSTGRES_HOST":        "postgres",
		"POSTGRES_PORT":        "5432",
		"POSTGRES_USER":        "postgres",
		"POSTGRES_PASSWORD":    "password",
		"POSTGRES_DB":          "embedding",
		"POSTGRES_SSLMODE":     "disable",
		"S3_ENDPOINT_URL":      "https://s3.example.com",
		"S3_BUCKET":            "bucket",
		"S3_REGION":            "auto",
		"S3_ACCESS_KEY_ID":     "access-key",
		"S3_SECRET_ACCESS_KEY": "secret-key",
		"S3_PREFIX":            "jobs",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}
