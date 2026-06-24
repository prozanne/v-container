package config

import "testing"

func TestLoadMissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	def := Defaults()
	if cfg != def {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
}

func TestSaveLoadMergesWithDefaults(t *testing.T) {
	dir := t.TempDir()
	// Save a config that only sets a couple of fields.
	partial := Config{DefaultCPUs: 8, ProxyMode: ProxyNone}
	if err := Save(dir, partial); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultCPUs != 8 {
		t.Errorf("DefaultCPUs = %d, want 8", cfg.DefaultCPUs)
	}
	if cfg.ProxyMode != ProxyNone {
		t.Errorf("ProxyMode = %q, want none", cfg.ProxyMode)
	}
	// Unset fields fall back to defaults.
	if cfg.DefaultMemMB != Defaults().DefaultMemMB {
		t.Errorf("DefaultMemMB = %d, want default %d", cfg.DefaultMemMB, Defaults().DefaultMemMB)
	}
	if cfg.GuestUser != Defaults().GuestUser {
		t.Errorf("GuestUser = %q, want default %q", cfg.GuestUser, Defaults().GuestUser)
	}
}
