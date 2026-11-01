package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Modos de arranque. Umbral prepara la instancia; la cuenta de Minecraft la
// gestiona otro: nunca ve ni guarda credenciales de otros launchers.
const (
	modeOfficial  = "oficial"   // perfil en el launcher oficial de Minecraft
	modeExternal  = "externo"   // Prism, ATLauncher... apuntando a la instancia
	modeMicrosoft = "microsoft" // inicio directo, solo si se compiló con ID de Azure
)

// launchRequest es lo que un modo necesita para abrir el juego.
type launchRequest struct {
	paths   Paths
	cfg     Config
	java    string
	vanilla *VersionJSON
	forgeID string
	name    string
}

// launchResult dice qué pasó: un aviso para el jugador y, si el juego corre
// como hijo de Umbral, cómo esperarlo.
type launchResult struct {
	notice string
	wait   func() error
	log    string
}

// officialProfileID es la clave del perfil que Umbral administra.
const officialProfileID = "umbral-sylvanor3"

func launchOfficial(_ context.Context, r launchRequest) (*launchResult, error) {
	dir := r.cfg.officialDir()
	if err := setupOfficialProfile(dir, r); err != nil {
		return nil, err
	}
	if err := openOfficialLauncher(); err != nil {
		return &launchResult{notice: "Perfil «" + r.name + "» creado. No encontré el launcher oficial: instálalo o ábrelo y elige ese perfil."}, nil
	}
	return &launchResult{notice: "Se abrió el launcher oficial. Elige el perfil «" + r.name + "» y pulsa Jugar. Tu cuenta la gestiona Minecraft, no Umbral."}, nil
}

func launchExternal(_ context.Context, r launchRequest) (*launchResult, error) {
	return &launchResult{notice: "Instancia lista en " + r.paths.Instance + ". Apunta tu launcher a esa carpeta con Forge " + r.forgeID + " y Java en " + r.java + "."}, nil
}

// setupOfficialProfile da de alta la versión y el perfil en la carpeta del
// launcher oficial. Solo añade: no reemplaza archivos existentes ni otros perfiles.
func setupOfficialProfile(dir string, r launchRequest) error {
	var profiles []string
	for _, name := range []string{"launcher_profiles.json", "launcher_profiles_microsoft_store.json"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			profiles = append(profiles, p)
		}
	}
	if len(profiles) == 0 {
		return fmt.Errorf("no encontré el launcher oficial en %s; ábrelo una vez o corrige la carpeta de Minecraft en Ajustes", dir)
	}
	hub.stage("Registrando Forge en el launcher oficial")
	src := filepath.Join(r.paths.Versions, r.forgeID, r.forgeID+".json")
	if err := copyMissing(src, filepath.Join(dir, "versions", r.forgeID, r.forgeID+".json")); err != nil {
		return err
	}
	err := filepath.WalkDir(r.paths.Libraries, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(r.paths.Libraries, p)
		if err != nil {
			return err
		}
		return copyMissing(p, filepath.Join(dir, "libraries", rel))
	})
	if err != nil {
		return fmt.Errorf("no se pudieron copiar las librerías de Forge: %w", err)
	}
	for _, p := range profiles {
		if err := writeProfile(p, r); err != nil {
			return err
		}
	}
	return nil
}

func writeProfile(path string, r launchRequest) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s está dañado; no lo toco: %w", path, err)
	}
	profiles, _ := doc["profiles"].(map[string]any)
	if profiles == nil {
		profiles = map[string]any{}
		doc["profiles"] = profiles
	}
	now := time.Now().UTC().Format(time.RFC3339)
	created := now
	if old, ok := profiles[officialProfileID].(map[string]any); ok {
		if c, ok := old["created"].(string); ok {
			created = c
		}
	}
	profiles[officialProfileID] = map[string]any{
		"name":          r.name,
		"type":          "custom",
		"created":       created,
		"lastUsed":      now,
		"lastVersionId": r.forgeID,
		"gameDir":       r.paths.Instance,
		"javaDir":       gameJavaBinary(r.java),
		"javaArgs":      strings.Join(memoryArgs(r.cfg), " "),
		"icon":          "Furnace",
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := copyMissing(path, path+".umbral-respaldo"); err != nil {
		return err
	}
	tmp := path + ".umbral-tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func copyMissing(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".umbral-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// openOfficialLauncher abre el launcher oficial, de escritorio o de la Tienda.
func openOfficialLauncher() error {
	if runtime.GOOS != "windows" {
		return errors.New("solo disponible en Windows")
	}
	for _, p := range []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Minecraft Launcher", "MinecraftLauncher.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Minecraft Launcher", "MinecraftLauncher.exe"),
	} {
		if _, err := os.Stat(p); err == nil {
			openPath(p)
			return nil
		}
	}
	const pkg = "Microsoft.4297127D64EC6_8wekyb3d8bbwe"
	if _, err := os.Stat(filepath.Join(os.Getenv("LOCALAPPDATA"), "Packages", pkg)); err == nil {
		log.Println("Abriendo el launcher oficial de la Tienda")
		openPath(`shell:AppsFolder\` + pkg + "!Minecraft")
		return nil
	}
	return errors.New("launcher oficial no encontrado")
}
