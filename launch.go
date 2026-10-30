package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// startGame abre Minecraft en segundo plano. La salida del juego va a
// logs/umbral-juego.log para poder diagnosticar cierres.
func startGame(paths Paths, cfg Config, acc *Account, java string, vanilla *VersionJSON, forgeID string) (wait func() error, logPath string, err error) {
	args, err := buildLaunchArgs(paths, cfg, acc, vanilla, forgeID)
	if err != nil {
		return nil, "", err
	}
	logDir := filepath.Join(paths.Instance, "logs")
	os.MkdirAll(logDir, 0o755)
	logPath = filepath.Join(logDir, "umbral-juego.log")
	out, err := os.Create(logPath)
	if err != nil {
		return nil, "", err
	}
	cmd := exec.Command(gameJavaBinary(java), args...)
	cmd.Dir = paths.Instance
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		out.Close()
		return nil, "", fmt.Errorf("no se pudo abrir Minecraft: %w", err)
	}
	wait = func() error {
		defer out.Close()
		return cmd.Wait()
	}
	return wait, logPath, nil
}

// buildLaunchArgs combina la versión de Forge con la versión base de
// Minecraft, como hace el launcher oficial, y devuelve los argumentos de java.
func buildLaunchArgs(paths Paths, cfg Config, acc *Account, vanilla *VersionJSON, forgeID string) ([]string, error) {
	forge, err := readVersion(paths, forgeID)
	if err != nil {
		return nil, err
	}

	// Forge espera el jar del juego con el nombre de su propia versión.
	vanillaJar := filepath.Join(paths.Versions, vanilla.ID, vanilla.ID+".jar")
	gameJar := filepath.Join(paths.Versions, forgeID, forgeID+".jar")
	if err := copyIfDifferent(vanillaJar, gameJar); err != nil {
		return nil, err
	}

	nativesDir := filepath.Join(paths.Versions, forgeID, "natives")
	os.MkdirAll(nativesDir, 0o755)

	// Librerías de Forge primero; si ambas versiones traen la misma
	// librería, gana la de Forge.
	var classpath []string
	seen := map[string]bool{}
	for _, v := range []*VersionJSON{forge, vanilla} {
		for _, lib := range v.Libraries {
			a, native := libArtifact(lib)
			if a == nil {
				continue
			}
			key := libraryKey(lib.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			path := filepath.Join(paths.Libraries, a.Path)
			classpath = append(classpath, path)
			if native {
				if err := extractNatives(path, nativesDir); err != nil {
					return nil, fmt.Errorf("no se pudieron extraer nativos de %s: %w", lib.Name, err)
				}
			}
		}
	}
	classpath = append(classpath, gameJar)

	vars := map[string]string{
		"auth_player_name":     acc.Name,
		"auth_uuid":            acc.UUID,
		"auth_access_token":    acc.AccessToken,
		"auth_xuid":            "",
		"clientid":             "",
		"user_type":            "msa",
		"version_name":         forgeID,
		"version_type":         "release",
		"game_directory":       paths.Instance,
		"assets_root":          paths.Assets,
		"assets_index_name":    vanilla.AssetIndex.ID,
		"natives_directory":    nativesDir,
		"launcher_name":        "umbral",
		"launcher_version":     launcherVersion,
		"classpath":            strings.Join(classpath, string(os.PathListSeparator)),
		"classpath_separator":  string(os.PathListSeparator),
		"library_directory":    paths.Libraries,
		"quickPlayMultiplayer": cfg.Server,
	}
	features := map[string]bool{"is_quick_play_multiplayer": cfg.Server != ""}

	args := []string{
		fmt.Sprintf("-Xmx%dm", cfg.MemoryMB),
		fmt.Sprintf("-Xms%dm", min(cfg.MemoryMB, 2048)),
	}
	args = append(args, resolveArgs(vanilla.Arguments.JVM, vars, features)...)
	args = append(args, resolveArgs(forge.Arguments.JVM, vars, features)...)
	args = append(args, forge.MainClass)
	args = append(args, resolveArgs(vanilla.Arguments.Game, vars, features)...)
	args = append(args, resolveArgs(forge.Arguments.Game, vars, features)...)
	return args, nil
}

// libraryKey es grupo:artefacto(:clasificador), sin la versión.
func libraryKey(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) >= 4 {
		return parts[0] + ":" + parts[1] + ":" + parts[3]
	}
	if len(parts) >= 2 {
		return parts[0] + ":" + parts[1]
	}
	return name
}

var varPattern = regexp.MustCompile(`\$\{([a-zA-Z_]+)\}`)

// resolveArgs procesa los argumentos de una versión: texto simple u
// objetos con reglas y valor (uno o varios textos).
func resolveArgs(raw []json.RawMessage, vars map[string]string, features map[string]bool) []string {
	var out []string
	expand := func(s string) string {
		return varPattern.ReplaceAllStringFunc(s, func(m string) string {
			if v, ok := vars[m[2:len(m)-1]]; ok {
				return v
			}
			return m
		})
	}
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			out = append(out, expand(s))
			continue
		}
		var obj struct {
			Rules []Rule          `json:"rules"`
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(r, &obj) != nil || !rulesAllow(obj.Rules, features) {
			continue
		}
		var values []string
		if json.Unmarshal(obj.Value, &s) == nil {
			values = []string{s}
		} else {
			json.Unmarshal(obj.Value, &values)
		}
		for _, v := range values {
			out = append(out, expand(v))
		}
	}
	return out
}

func copyIfDifferent(src, dst string) error {
	a, err := fileSHA1(src)
	if err != nil {
		return err
	}
	if b, err := fileSHA1(dst); err == nil && a == b {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	os.MkdirAll(filepath.Dir(dst), 0o755)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func extractNatives(jarPath, dest string) error {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := f.Name
		if strings.HasPrefix(name, "META-INF/") || f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".dll" && ext != ".so" && ext != ".dylib" && ext != ".jnilib" {
			continue
		}
		target := filepath.Join(dest, filepath.Base(name))
		if _, err := os.Stat(target); err == nil {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
