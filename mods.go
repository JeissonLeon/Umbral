package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ensureMods deja en la carpeta mods exactamente el modpack base y los
// extras que el jugador activó. Cualquier otro .jar se mueve a
// mods-desactivados (no se borra), porque el servidor solo admite la lista
// permitida.
func ensureMods(paths Paths, base, extras []Mod) error {
	hub.stage("Comprobando mods")
	dir := filepath.Join(paths.Instance, "mods")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	wanted := map[string]bool{}
	var jobs []Job
	for _, m := range append(append([]Mod{}, base...), extras...) {
		wanted[m.File] = true
		jobs = append(jobs, Job{URL: m.URL, Dest: filepath.Join(dir, m.File), SHA1: m.SHA1})
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	disabled := filepath.Join(paths.Instance, "mods-desactivados")
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || wanted[name] || !strings.HasSuffix(strings.ToLower(name), ".jar") {
			continue
		}
		if err := os.MkdirAll(disabled, 0o755); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(dir, name), filepath.Join(disabled, name)); err != nil {
			return fmt.Errorf("no se pudo apartar el mod %s: %w", name, err)
		}
		log.Println("Mod no permitido movido a mods-desactivados:", name)
	}

	return downloadAll("Mods", pending(jobs))
}
