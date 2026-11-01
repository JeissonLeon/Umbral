package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"umbral/internal/pack"
)

// quarantineForeignMods mueve a mods-desactivados (sin borrarlos) los .jar que
// no son del modpack: un mod ajeno puede impedir arrancar o entrar al servidor.
func quarantineForeignMods(paths Paths, m *pack.Manifest, enabled func(string) bool) error {
	dir := filepath.Join(paths.Instance, "mods")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	wanted := map[string]bool{}
	for _, f := range m.Files {
		if f.Required || enabled(f.Path) {
			wanted[strings.ToLower(f.Path)] = true
		}
	}
	disabled := filepath.Join(paths.Instance, "mods-desactivados")
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || wanted["mods/"+strings.ToLower(name)] || !strings.HasSuffix(strings.ToLower(name), ".jar") {
			continue
		}
		if err := os.MkdirAll(disabled, 0o755); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(dir, name), filepath.Join(disabled, name)); err != nil {
			return fmt.Errorf("no se pudo apartar el mod %s: %w", name, err)
		}
		log.Println("Mod ajeno al modpack movido a mods-desactivados:", name)
	}
	return nil
}
