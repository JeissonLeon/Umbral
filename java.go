package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const javaRuntimesURL = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

func javaPlatform() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "windows-x64", nil
	case "windows/arm64":
		return "windows-arm64", nil
	case "windows/386":
		return "windows-x86", nil
	case "darwin/amd64":
		return "mac-os", nil
	case "darwin/arm64":
		return "mac-os-arm64", nil
	case "linux/amd64":
		return "linux", nil
	case "linux/386":
		return "linux-i386", nil
	}
	return "", fmt.Errorf("sistema no soportado: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func javaBinary(home string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "bin", "java.exe")
	case "darwin":
		return filepath.Join(home, "jre.bundle", "Contents", "Home", "bin", "java")
	}
	return filepath.Join(home, "bin", "java")
}

// gameJavaBinary es el Java para el juego: en Windows javaw.exe, que no
// abre ventana de consola.
func gameJavaBinary(java string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(filepath.Dir(java), "javaw.exe")
	}
	return java
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ensureJava descarga el Java oficial que Mojang publica para esta versión
// del juego, así el jugador no necesita instalar nada.
func ensureJava(paths Paths, component string) (string, error) {
	if component == "" {
		component = "java-runtime-gamma"
	}
	hub.stage("Comprobando Java")
	platform, err := javaPlatform()
	if err != nil {
		return "", err
	}
	var all map[string]map[string][]struct {
		Manifest struct {
			URL string `json:"url"`
		} `json:"manifest"`
	}
	if err := getJSON(javaRuntimesURL, &all); err != nil {
		// Sin conexión vale el Java ya descargado, para poder jugar igual.
		if bin := javaBinary(filepath.Join(paths.Runtime, component)); fileExists(bin) {
			return bin, nil
		}
		return "", fmt.Errorf("no se pudo leer la lista de Java de Mojang: %w", err)
	}
	entries := all[platform][component]
	if len(entries) == 0 {
		return "", fmt.Errorf("Mojang no publica %s para %s", component, platform)
	}
	var manifest struct {
		Files map[string]struct {
			Type       string `json:"type"`
			Executable bool   `json:"executable"`
			Target     string `json:"target"`
			Downloads  struct {
				Raw struct {
					URL  string `json:"url"`
					SHA1 string `json:"sha1"`
				} `json:"raw"`
			} `json:"downloads"`
		} `json:"files"`
	}
	if err := getJSON(entries[0].Manifest.URL, &manifest); err != nil {
		return "", err
	}

	home := filepath.Join(paths.Runtime, component)
	var jobs []Job
	var executables []string
	for name, f := range manifest.Files {
		dest := filepath.Join(home, filepath.FromSlash(name))
		switch f.Type {
		case "directory":
			os.MkdirAll(dest, 0o755)
		case "file":
			jobs = append(jobs, Job{URL: f.Downloads.Raw.URL, Dest: dest, SHA1: f.Downloads.Raw.SHA1})
			if f.Executable {
				executables = append(executables, dest)
			}
		case "link":
			if runtime.GOOS != "windows" {
				os.MkdirAll(filepath.Dir(dest), 0o755)
				os.Remove(dest)
				os.Symlink(f.Target, dest)
			}
		}
	}
	if err := downloadAll("Java", pending(jobs)); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		for _, e := range executables {
			os.Chmod(e, 0o755)
		}
	}
	bin := javaBinary(home)
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("Java descargado pero falta %s", bin)
	}
	return bin, nil
}
