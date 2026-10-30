package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
)

type Manifest struct {
	Name           string   `json:"nombre"`
	ModpackVersion string   `json:"version_modpack"`
	Minecraft      string   `json:"minecraft"`
	Forge          string   `json:"forge"`
	News           []string `json:"novedades"`
	Mods           []Mod    `json:"mods"`
	Extras         []Mod    `json:"extras"`
}

type Mod struct {
	Name string `json:"nombre"`
	Desc string `json:"descripcion"`
	Side string `json:"lado"`
	File string `json:"archivo"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
}

func parseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Minecraft == "" || m.Forge == "" {
		return m, errors.New("al manifiesto le falta la versión de Minecraft o de Forge")
	}
	for _, mod := range append(append([]Mod{}, m.Mods...), m.Extras...) {
		if mod.File == "" || mod.URL == "" || len(mod.SHA1) != 40 || filepath.Base(mod.File) != mod.File {
			return m, errors.New("el manifiesto tiene un mod sin archivo, URL o huella válida")
		}
	}
	return m, nil
}

func builtinManifest() Manifest {
	m, err := parseManifest(manifestJSON)
	if err != nil {
		panic("manifiesto interno dañado: " + err.Error())
	}
	return m
}

func manifestCache(paths Paths) string { return filepath.Join(paths.Root, "manifiesto-remoto.json") }

// cachedManifest devuelve la última copia del manifiesto remoto, o el interno
// si no hay. No usa la red, para que la interfaz arranque al instante.
func cachedManifest(paths Paths) Manifest {
	if data, err := os.ReadFile(manifestCache(paths)); err == nil {
		if m, err := parseManifest(data); err == nil {
			return m
		}
	}
	return builtinManifest()
}

// loadManifest descarga el manifiesto remoto si hay URL y lo guarda; si no
// se puede, usa la última copia o el interno.
func loadManifest(paths Paths, src string) Manifest {
	if src == "" {
		return builtinManifest()
	}
	var raw json.RawMessage
	if err := getJSON(src, &raw); err != nil {
		log.Println("No se pudo descargar el manifiesto remoto:", err)
		return cachedManifest(paths)
	}
	m, err := parseManifest(raw)
	if err != nil {
		log.Println("Manifiesto remoto no válido:", err)
		return cachedManifest(paths)
	}
	os.WriteFile(manifestCache(paths), raw, 0o644)
	return m
}
