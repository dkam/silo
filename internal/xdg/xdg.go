package xdg

import (
	"os"
	"path/filepath"
)

// DataHome returns $XDG_DATA_HOME/subdir, falling back to ~/.local/share/subdir.
func DataHome(subdir string) (string, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, subdir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", subdir), nil
}

// ConfigHome returns $XDG_CONFIG_HOME/subdir, falling back to ~/.config/subdir.
func ConfigHome(subdir string) (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, subdir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", subdir), nil
}
