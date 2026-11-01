package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"umbral/internal/pack"
)

func init() {
	hashRemote = func(_ context.Context, u, _ string) (string, int64, error) {
		if strings.Contains(u, "falla") {
			return "", 0, errors.New("sin conexión")
		}
		return strings.Repeat("a", 64), 10, nil
	}
}

func config(t *testing.T, files map[string]string) Config {
	dir := t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
	}
	var c Config
	c.Modpack.ID, c.Modpack.Name, c.Modpack.Minecraft, c.Modpack.Forge, c.Modpack.Java = "sylvanor3", "SylvanΩr 3", "1.20.1", "47.4.10", 17
	c.Content = dir
	return c
}

func opts(t *testing.T, v string) buildOptions {
	return buildOptions{Version: v, LauncherVersion: "1.0.0", Out: filepath.Join(t.TempDir(), "out"), BaseURL: "https://github.com/o/r/releases/download"}
}

func TestFirstRelease(t *testing.T) {
	c := config(t, map[string]string{"mods/umbral_sombras-0.3.0.jar": "umbral", "config/a.toml": "x", "mods/.gitkeep": ""})
	c.UmbralMod = `^mods/umbral_sombras-(.+)\.jar$`
	c.Seed = []string{"config/a.toml"}
	c.External = []External{{Path: "mods/ext.jar", URL: "https://cdn.modrinth.com/ext.jar", Name: "Ext", Optional: true}}
	res, err := build(context.Background(), c, nil, opts(t, "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	m := res.Manifest
	if m.Sequence != 1 || len(m.Files) != 3 || m.UmbralMod.Version != "0.3.0" {
		t.Fatalf("%+v", m)
	}
	if len(res.Upload) != 2 {
		t.Fatalf("subidas: %v", res.Upload)
	}
	for _, f := range m.Files {
		switch f.Path {
		case "config/a.toml":
			if f.Policy != pack.PolicySeed || !f.Required {
				t.Errorf("%+v", f)
			}
		case "mods/ext.jar":
			if f.Required || f.URL != "https://cdn.modrinth.com/ext.jar" {
				t.Errorf("%+v", f)
			}
		default:
			if !strings.HasPrefix(f.URL, "https://github.com/o/r/releases/download/sylvanor3-1.0.0-1/") {
				t.Errorf("%+v", f)
			}
		}
	}
	data, _ := json.Marshal(m)
	if _, err := pack.Parse(data); err != nil {
		t.Fatal(err)
	}
}

func TestSecondReleaseIsIncremental(t *testing.T) {
	c1 := config(t, map[string]string{"mods/igual.jar": "igual", "mods/cambia.jar": "v1", "mods/retirado.jar": "r"})
	first, err := build(context.Background(), c1, nil, opts(t, "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	c2 := config(t, map[string]string{"mods/igual.jar": "igual", "mods/cambia.jar": "v2"})
	res, err := build(context.Background(), c2, first.Manifest, opts(t, "1.1.0"))
	if err != nil {
		t.Fatal(err)
	}
	m := res.Manifest
	if m.Sequence != 2 || len(res.Upload) != 1 || !strings.Contains(res.Upload[0], "cambia") {
		t.Fatalf("seq %d, subidas %v", m.Sequence, res.Upload)
	}
	if len(m.Remove) != 1 || m.Remove[0].Path != "mods/retirado.jar" || m.Remove[0].SHA256 == "" {
		t.Fatalf("remove: %+v", m.Remove)
	}
	for _, f := range m.Files {
		if f.Path == "mods/igual.jar" && !strings.Contains(f.URL, "sylvanor3-1.0.0-1") {
			t.Errorf("debe reutilizar la URL anterior: %s", f.URL)
		}
	}
	if m.Launcher.Version != "1.0.0" {
		t.Error("debe conservar la versión del launcher")
	}
	// Si el mod vuelve, sale de remove.
	c3 := config(t, map[string]string{"mods/igual.jar": "igual", "mods/retirado.jar": "r"})
	res3, err := build(context.Background(), c3, m, opts(t, "1.2.0"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res3.Manifest.Remove {
		if r.Path == "mods/retirado.jar" {
			t.Error("la definición nueva debe prevalecer")
		}
	}
}

func TestLauncherAssets(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "UmbralLauncher.exe"), []byte("exe"), 0o644)
	os.WriteFile(filepath.Join(dir, "umbral-updater.exe"), []byte("upd"), 0o644)
	o := opts(t, "1.0.0")
	o.LauncherDir, o.LauncherVersion = dir, "2.0.0"
	res, err := build(context.Background(), config(t, map[string]string{"mods/a.jar": "a"}), nil, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Manifest.Launcher.Version != "2.0.0" || len(res.Manifest.Launcher.Files) != 2 {
		t.Fatalf("%+v", res.Manifest.Launcher)
	}
}

func TestBuildRejects(t *testing.T) {
	bad := config(t, map[string]string{"saves/mundo.dat": "x"})
	if _, err := build(context.Background(), bad, nil, opts(t, "1")); err == nil {
		t.Error("una carpeta no permitida debe rechazarse")
	}
	empty := config(t, nil)
	if _, err := build(context.Background(), empty, nil, opts(t, "1")); err == nil {
		t.Error("una versión vacía debe rechazarse")
	}
	dup := config(t, map[string]string{"mods/a.jar": "a"})
	dup.External = []External{{Path: "mods/a.jar", URL: "https://x.test/a"}}
	if _, err := build(context.Background(), dup, nil, opts(t, "1")); err == nil {
		t.Error("una ruta duplicada debe rechazarse")
	}
	down := config(t, map[string]string{"mods/a.jar": "a"})
	down.External = []External{{Path: "mods/b.jar", URL: "https://x.test/falla"}}
	if _, err := build(context.Background(), down, nil, opts(t, "1")); err == nil {
		t.Error("un externo inaccesible debe detener la publicación")
	}
	noMod := config(t, map[string]string{"mods/a.jar": "a"})
	noMod.UmbralMod = `^mods/umbral`
	if _, err := build(context.Background(), noMod, nil, opts(t, "1")); err == nil {
		t.Error("debe exigir el mod Umbral")
	}
}

func TestPublishGoesLiveLast(t *testing.T) {
	m := &pack.Manifest{Sequence: 3, Modpack: pack.Modpack{ID: "s", Name: "S", Version: "1"}}
	files := make([]string, 45)
	cmds := publishCommands("o/r", m, files)
	if cmds[0][1] != "create" || !contains(cmds[0], "--draft") {
		t.Fatalf("%v", cmds[0])
	}
	if len(cmds) != 5 {
		t.Fatalf("lotes: %d", len(cmds))
	}
	last := cmds[len(cmds)-1]
	if last[1] != "edit" || !contains(last, "--draft=false") || !contains(last, "--latest") {
		t.Fatalf("%v", last)
	}
}

func TestAssetName(t *testing.T) {
	if got := assetName("embeddium-0.3+mc 1.20Ω.jar"); got != "embeddium-0.3_mc_1.20_.jar" {
		t.Fatal(got)
	}
}
