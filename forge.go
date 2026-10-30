package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ensureForge instala Forge con su instalador oficial en modo cliente y
// devuelve el id de la versión instalada (por ejemplo 1.20.1-forge-47.4.10).
func ensureForge(paths Paths, java, mcVersion, forgeVersion string) (string, error) {
	id := mcVersion + "-forge-" + forgeVersion
	hub.stage("Comprobando Forge " + forgeVersion)

	if v, err := readVersion(paths, id); err == nil {
		if missing := missingForgeLibraries(paths, v); len(missing) == 0 {
			return id, nil
		}
		log.Println("Faltan archivos de Forge, se reinstala")
	}

	full := mcVersion + "-" + forgeVersion
	base := "https://maven.minecraftforge.net/net/minecraftforge/forge/" + full + "/forge-" + full + "-installer.jar"
	installer := filepath.Join(paths.Cache, "forge-"+full+"-installer.jar")
	sum, err := fetchText(base + ".sha1")
	if err != nil {
		return "", fmt.Errorf("no se pudo consultar Forge: %w", err)
	}
	if err := download(Job{URL: base, Dest: installer, SHA1: sum}); err != nil {
		return "", err
	}

	// El instalador exige un launcher_profiles.json en la carpeta destino.
	profiles := filepath.Join(paths.Root, "launcher_profiles.json")
	if _, err := os.Stat(profiles); err != nil {
		if err := os.WriteFile(profiles, []byte(`{"profiles":{}}`), 0o644); err != nil {
			return "", err
		}
	}

	hub.stage("Instalando Forge (la primera vez tarda unos minutos)")
	cmd := exec.Command(java, "-jar", installer, "--installClient", paths.Root)
	cmd.Dir = paths.Cache
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	logPath := filepath.Join(paths.Cache, "forge-instalacion.log")
	os.WriteFile(logPath, out, 0o644)
	if err != nil {
		return "", fmt.Errorf("el instalador de Forge falló (detalles en %s): %w", logPath, err)
	}

	v, err := readVersion(paths, id)
	if err != nil {
		return "", fmt.Errorf("Forge se instaló pero no aparece la versión %s (detalles en %s)", id, logPath)
	}
	if missing := missingForgeLibraries(paths, v); len(missing) > 0 {
		return "", fmt.Errorf("faltan librerías de Forge tras instalar: %s", strings.Join(missing, ", "))
	}
	return id, nil
}

// missingForgeLibraries descarga lo que falte con URL y devuelve lo que
// siga faltando (archivos que solo genera el instalador).
func missingForgeLibraries(paths Paths, v *VersionJSON) []string {
	var missing []string
	for _, lib := range v.Libraries {
		a, _ := libArtifact(lib)
		if a == nil {
			continue
		}
		dest := filepath.Join(paths.Libraries, a.Path)
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		if a.URL == "" || download(Job{URL: a.URL, Dest: dest, SHA1: a.SHA1}) != nil {
			missing = append(missing, lib.Name)
		}
	}
	return missing
}

func fetchText(url string) (string, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s respondió %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
