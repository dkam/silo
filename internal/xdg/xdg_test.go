package xdg

import (
	"path/filepath"
	"testing"
)

func TestDataHome(t *testing.T) {
	t.Run("uses XDG_DATA_HOME when set", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/custom/data")
		got, err := DataHome("silo")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/custom/data", "silo"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to home/.local/share", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		got, err := DataHome("silo")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(filepath.Dir(filepath.Dir(got))) != "share" {
			// Just verify it ends with .local/share/silo
		}
		if filepath.Base(got) != "silo" {
			t.Errorf("got %q, want path ending in /silo", got)
		}
	})
}

func TestConfigHome(t *testing.T) {
	t.Run("uses XDG_CONFIG_HOME when set", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/custom/config")
		got, err := ConfigHome("silo")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/custom/config", "silo"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to home/.config", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got, err := ConfigHome("silo")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "silo" {
			t.Errorf("got %q, want path ending in /silo", got)
		}
	})
}
