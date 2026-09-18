package main

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

func vaultDir() string {
	return filepath.Join(xdg.DataHome, "clical", "vault")
}

func dbPath() string {
	return filepath.Join(xdg.DataHome, "clical", "clical.db")
}
