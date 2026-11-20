package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[redact]\npatterns = ['acme-\\w+']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || !slices.Equal(c.Redact.Patterns, []string{`acme-\w+`}) {
		t.Fatalf("Load = %+v, %v", c, err)
	}
}

func TestLoadMissing(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || c.Redact.Patterns != nil {
		t.Fatalf("Load = %+v, %v", c, err)
	}
}

func TestLoadInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[redact\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("want error")
	}
}

func TestInit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tidemark", "config.toml")
	if created, err := Init(path); err != nil || !created {
		t.Fatalf("Init = %v, %v", created, err)
	}
	if c, err := Load(path); err != nil || c.Redact.Patterns != nil {
		t.Fatalf("Load = %+v, %v", c, err)
	}
	if err := os.WriteFile(path, []byte("[redact]\npatterns = ['x']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if created, err := Init(path); err != nil || created {
		t.Fatalf("second Init = %v, %v", created, err)
	}
	if c, _ := Load(path); !slices.Equal(c.Redact.Patterns, []string{"x"}) {
		t.Errorf("existing config overwritten: %+v", c)
	}
}
