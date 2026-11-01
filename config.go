package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Config se guarda en config.json dentro de la carpeta de datos.
type Config struct {
	// Solo para quien compila su launcher: sustituye el ID de aplicación.
	ClientID     string   `json:"microsoft_client_id,omitempty"`
	Server       string   `json:"servidor"`
	MemoryMB     int      `json:"memoria_mb"`
	ManifestURL  string   `json:"manifiesto_url,omitempty"`
	Extras       []string `json:"mods_extra"`
	LaunchMode   string   `json:"modo_inicio,omitempty"`
	MinecraftDir string   `json:"carpeta_minecraft,omitempty"`
	JavaPath     string   `json:"java_ruta,omitempty"`
	JavaArgs     string   `json:"java_argumentos,omitempty"`
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
	tmp := configFile(paths) + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, configFile(paths))
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

// mode es el modo de arranque efectivo. El inicio directo solo existe si el
// launcher se compiló con un ID de aplicación aprobado.
func (c Config) mode() string {
	switch c.LaunchMode {
	case modeExternal:
		return modeExternal
	case modeMicrosoft:
		if c.clientID() != "" {
			return modeMicrosoft
		}
	}
	return modeOfficial
}

func (c Config) officialDir() string {
	if c.MinecraftDir != "" {
		return c.MinecraftDir
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return ".minecraft"
	}
	return filepath.Join(base, ".minecraft")
}

// memoryArgs son los argumentos de Java comunes a todos los modos.
func memoryArgs(c Config) []string {
	args := []string{fmt.Sprintf("-Xmx%dm", c.MemoryMB), fmt.Sprintf("-Xms%dm", min(c.MemoryMB, 2048))}
	return append(args, strings.Fields(c.JavaArgs)...)
}

var javaVersionPattern = regexp.MustCompile(`version "(\d+)(?:\.(\d+))?`)

// javaMajor extrae la versión de la salida de java -version.
func javaMajor(output string) (int, error) {
	g := javaVersionPattern.FindStringSubmatch(output)
	if g == nil {
		return 0, errors.New("no se reconoce la salida de java -version")
	}
	n, err := strconv.Atoi(g[1])
	if err != nil {
		return 0, err
	}
	if n == 1 && g[2] != "" { // formato antiguo: 1.8
		return strconv.Atoi(g[2])
	}
	return n, nil
}

// checkJava comprueba que una ruta elegida por el jugador sea el Java pedido.
func checkJava(path string, want int) error {
	if !filepath.IsAbs(path) {
		return errors.New("la ruta de Java debe ser completa")
	}
	cmd := exec.Command(path, "-version")
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("no se pudo ejecutar Java en %s: %w", path, err)
	}
	got, err := javaMajor(string(out))
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%s es Java %d; este modpack necesita Java %d", path, got, want)
	}
	return nil
}
