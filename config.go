package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config se guarda en config.json dentro de la carpeta de datos.
type Config struct {
	// Solo para pruebas: sustituye el ID de aplicación con el que se compiló.
	ClientID    string   `json:"microsoft_client_id,omitempty"`
	Server      string   `json:"servidor"`
	MemoryMB    int      `json:"memoria_mb"`
	ManifestURL string   `json:"manifiesto_url,omitempty"`
	Extras      []string `json:"mods_extra"`
}

func configFile(paths Paths) string { return filepath.Join(paths.Root, "config.json") }

func loadConfig(paths Paths) (Config, error) {
	cfg := Config{MemoryMB: 6144, Extras: []string{}}
	data, err := os.ReadFile(configFile(paths))
	if os.IsNotExist(err) {
		return cfg, saveConfig(paths, cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s no es JSON válido: %w", configFile(paths), err)
	}
	cfg.MemoryMB = clampMemory(cfg.MemoryMB)
	if cfg.Extras == nil {
		cfg.Extras = []string{}
	}
	return cfg, nil
}

func saveConfig(paths Paths, cfg Config) error {
	out, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configFile(paths), out, 0o644)
}

func clampMemory(mb int) int {
	return max(2048, min(mb, 16384))
}

func (c Config) manifestURL() string {
	if c.ManifestURL != "" {
		return c.ManifestURL
	}
	return defaultManifestURL
}

func (c Config) clientID() string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return defaultClientID
}
