package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"umbral/internal/pack"
)

// release sirve un manifiesto firmado, como GitHub Releases.
type release struct {
	*httptest.Server
	priv     ed25519.PrivateKey
	keys     []ed25519.PublicKey
	raw, sig []byte
	files    map[string]string
}

func newRelease(t *testing.T) *release {
	pub, priv, _ := ed25519.GenerateKey(nil)
	r := &release{priv: priv, keys: []ed25519.PublicKey{pub}, files: map[string]string{}}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		switch q.URL.Path {
		case "/manifest.json":
			w.Write(r.raw)
		case "/manifest.json.sig":
			w.Write(r.sig)
		default:
			if body, ok := r.files[q.URL.Path]; ok {
				w.Write([]byte(body))
				return
			}
			http.NotFound(w, q)
		}
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *release) publish(t *testing.T, seq int64, launcher string, files map[string]string) {
	m := pack.Manifest{SchemaVersion: 1, Sequence: seq,
		Modpack:  pack.Modpack{ID: "sylvanor3", Name: "SylvanΩr 3", Version: "v" + string(rune('0'+seq)), Minecraft: "1.20.1", Forge: "47.4.10", Java: 17},
		Launcher: pack.Launcher{Version: launcher},
		Remove:   []pack.Remove{},
	}
	for p, body := range files {
		tmp := filepath.Join(t.TempDir(), "f")
		os.WriteFile(tmp, []byte(body), 0o644)
		h, _ := pack.HashFile(tmp)
		r.files["/"+h] = body
		m.Files = append(m.Files, pack.File{Path: p, Version: "1", URL: r.URL + "/" + h, SHA256: h, Size: int64(len(body)), Required: true})
	}
	r.raw, _ = json.Marshal(m)
	r.sig, _ = pack.Sign(r.raw, r.priv)
}

func testPaths(t *testing.T) Paths {
	t.Setenv("UMBRAL_HOME", t.TempDir())
	p, err := newPaths()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFetchManifestVerifiesAndCaches(t *testing.T) {
	r, paths := newRelease(t), testPaths(t)
	r.publish(t, 2, "1.0.0", map[string]string{"mods/a.jar": "a"})
	m, err := fetchManifest(paths, r.URL+"/manifest.json", r.keys)
	if err != nil || m.Sequence != 2 {
		t.Fatal(m, err)
	}
	if c := cachedManifest(paths, r.keys); c == nil || c.Sequence != 2 {
		t.Fatal("no quedó la copia verificada")
	}
}

func TestFetchManifestRejectsBadSignatureAndKeepsCache(t *testing.T) {
	r, paths := newRelease(t), testPaths(t)
	r.publish(t, 1, "1.0.0", map[string]string{"mods/a.jar": "a"})
	if _, err := fetchManifest(paths, r.URL+"/manifest.json", r.keys); err != nil {
		t.Fatal(err)
	}
	r.raw = []byte(strings.Replace(string(r.raw), "47.4.10", "47.4.11", 1))
	if _, err := fetchManifest(paths, r.URL+"/manifest.json", r.keys); !errors.Is(err, pack.ErrBadSignature) {
		t.Fatalf("error: %v", err)
	}
	if c := cachedManifest(paths, r.keys); c == nil || c.Modpack.Forge != "47.4.10" {
		t.Fatal("la copia verificada no debe cambiar")
	}
}

func TestFetchManifestRejectsRollback(t *testing.T) {
	r, paths := newRelease(t), testPaths(t)
	r.publish(t, 5, "1.0.0", map[string]string{"mods/a.jar": "a"})
	if _, err := fetchManifest(paths, r.URL+"/manifest.json", r.keys); err != nil {
		t.Fatal(err)
	}
	r.publish(t, 4, "1.0.0", map[string]string{"mods/a.jar": "viejo"})
	if _, err := fetchManifest(paths, r.URL+"/manifest.json", r.keys); err == nil {
		t.Fatal("una secuencia menor, aunque firmada, debe rechazarse")
	}
}

func TestTamperedCacheIsIgnored(t *testing.T) {
	r, paths := newRelease(t), testPaths(t)
	r.publish(t, 1, "1.0.0", map[string]string{"mods/a.jar": "a"})
	fetchManifest(paths, r.URL+"/manifest.json", r.keys)
	mPath, _ := manifestCache(paths)
	data, _ := os.ReadFile(mPath)
	os.WriteFile(mPath, []byte(strings.Replace(string(data), "47.4.10", "47.4.11", 1)), 0o644)
	if cachedManifest(paths, r.keys) != nil {
		t.Fatal("una copia alterada en disco no debe aceptarse")
	}
}

func newTestApp(t *testing.T, r *release, paths Paths) *App {
	a := &App{paths: paths, token: "x", keys: r.keys, pingWake: make(chan struct{}, 1)}
	a.cfg = Config{MemoryMB: 4096, ManifestURL: r.URL + "/manifest.json"}
	a.installer = &pack.Installer{Root: paths.Instance, Dir: filepath.Join(paths.Root, "estado"),
		Enabled: func(string) bool { return false }}
	return a
}

// Detección: una versión nueva publicada aparece como pendiente, con sus cambios.
func TestRefreshDetectsUpdate(t *testing.T) {
	r, paths := newRelease(t), testPaths(t)
	r.publish(t, 1, "1.0.0", map[string]string{"mods/a.jar": "a", "mods/viejo.jar": "v"})
	a := newTestApp(t, r, paths)
	a.refresh()
	if s := a.snapshot(); !s.Update.Pending || len(s.Update.Add) != 2 {
		t.Fatalf("%+v", s.Update)
	}
	if err := a.installer.Apply(context.Background(), a.manifest.Manifest, a.plan); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	if s := a.snapshot(); s.Update.Pending || s.Update.Installed != "v1" {
		t.Fatalf("%+v", s.Update)
	}
	r.publish(t, 2, "1.1.0", map[string]string{"mods/a.jar": "a2", "mods/nuevo.jar": "n"})
	a.refresh()
	s := a.snapshot()
	if !s.Update.Pending || s.Update.Available != "v2" || len(s.Update.Add) != 1 || len(s.Update.Update) != 1 || len(s.Update.Delete) != 1 {
		t.Fatalf("%+v", s.Update)
	}
}

// Sin conexión sigue con la última copia y puede jugar.
func TestRefreshOfflineUsesCache(t *testing.T) {
	r, paths := newRelease(t), testPaths(t)
	r.publish(t, 1, "1.0.0", map[string]string{"mods/a.jar": "a"})
	a := newTestApp(t, r, paths)
	a.refresh()
	a.installer.Apply(context.Background(), a.manifest.Manifest, a.plan)
	r.Close()
	a.refresh()
	s := a.snapshot()
	if !s.Offline || s.Update.Pending || s.Error != "" {
		t.Fatalf("%+v", s)
	}
}

func TestQuarantineMovesForeignJarsOnly(t *testing.T) {
	paths := testPaths(t)
	mods := filepath.Join(paths.Instance, "mods")
	os.MkdirAll(mods, 0o755)
	os.WriteFile(filepath.Join(mods, "oficial.jar"), []byte("o"), 0o644)
	os.WriteFile(filepath.Join(mods, "ajeno.jar"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(mods, "nota.txt"), []byte("n"), 0o644)
	m := &pack.Manifest{Files: []pack.File{{Path: "mods/oficial.jar", Required: true}}}
	if err := quarantineForeignMods(paths, m, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(mods, "oficial.jar")) || !fileExists(filepath.Join(mods, "nota.txt")) {
		t.Fatal("tocó lo que no debía")
	}
	if fileExists(filepath.Join(mods, "ajeno.jar")) || !fileExists(filepath.Join(paths.Instance, "mods-desactivados", "ajeno.jar")) {
		t.Fatal("el mod ajeno debe moverse, no borrarse")
	}
}

func officialFixture(t *testing.T) (string, launchRequest) {
	paths := testPaths(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "launcher_profiles.json"), []byte(`{"profiles":{"mio":{"name":"Mío"}},"settings":{"x":1},"otra":true}`), 0o644)
	id := "1.20.1-forge-47.4.10"
	os.MkdirAll(filepath.Join(paths.Versions, id), 0o755)
	os.WriteFile(filepath.Join(paths.Versions, id, id+".json"), []byte("{}"), 0o644)
	lib := filepath.Join(paths.Libraries, "net", "x", "x.jar")
	os.MkdirAll(filepath.Dir(lib), 0o755)
	os.WriteFile(lib, []byte("nueva"), 0o644)
	return dir, launchRequest{paths: paths, cfg: Config{MemoryMB: 6144}, java: filepath.Join("java", "bin", "java.exe"), forgeID: id, name: "SylvanΩr 3"}
}

func TestOfficialProfileAddsWithoutOverwriting(t *testing.T) {
	dir, req := officialFixture(t)
	existing := filepath.Join(dir, "libraries", "net", "x", "x.jar")
	os.MkdirAll(filepath.Dir(existing), 0o755)
	os.WriteFile(existing, []byte("del jugador"), 0o644)

	if err := setupOfficialProfile(dir, req); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles map[string]map[string]any `json:"profiles"`
		Settings map[string]any            `json:"settings"`
		Otra     bool                      `json:"otra"`
	}
	data, _ := os.ReadFile(filepath.Join(dir, "launcher_profiles.json"))
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Profiles["mio"]["name"] != "Mío" || !doc.Otra || doc.Settings["x"] == nil {
		t.Fatalf("perdió datos ajenos: %s", data)
	}
	p := doc.Profiles[officialProfileID]
	if p["lastVersionId"] != req.forgeID || p["gameDir"] != req.paths.Instance {
		t.Fatalf("perfil: %v", p)
	}
	if !strings.Contains(p["javaArgs"].(string), "-Xmx6144m") {
		t.Fatalf("javaArgs: %v", p["javaArgs"])
	}
	if got, _ := os.ReadFile(existing); string(got) != "del jugador" {
		t.Fatal("sobrescribió un archivo existente")
	}
	if !fileExists(filepath.Join(dir, "versions", req.forgeID, req.forgeID+".json")) {
		t.Fatal("no copió la versión")
	}
	if !fileExists(filepath.Join(dir, "launcher_profiles.json.umbral-respaldo")) {
		t.Fatal("no dejó respaldo")
	}
}

func TestOfficialProfileRefusesWithoutLauncher(t *testing.T) {
	_, req := officialFixture(t)
	if err := setupOfficialProfile(t.TempDir(), req); err == nil {
		t.Fatal("sin launcher oficial debe avisar")
	}
}

func TestOfficialProfileRefusesDamagedFile(t *testing.T) {
	dir, req := officialFixture(t)
	p := filepath.Join(dir, "launcher_profiles.json")
	os.WriteFile(p, []byte("{ roto"), 0o644)
	if err := setupOfficialProfile(dir, req); err == nil {
		t.Fatal("debía negarse")
	}
	if got, _ := os.ReadFile(p); string(got) != "{ roto" {
		t.Fatal("no debe tocar un archivo que no entiende")
	}
}

func TestModeFallsBackWithoutClientID(t *testing.T) {
	if (Config{LaunchMode: modeMicrosoft}).mode() != modeOfficial {
		t.Fatal("sin ID de Azure no hay inicio directo")
	}
	if (Config{LaunchMode: modeMicrosoft, ClientID: "x"}).mode() != modeMicrosoft {
		t.Fatal("con ID sí")
	}
	if (Config{}).mode() != modeOfficial || (Config{LaunchMode: modeExternal}).mode() != modeExternal {
		t.Fatal("modo incorrecto")
	}
}

func TestJavaMajor(t *testing.T) {
	cases := map[string]int{
		`openjdk version "17.0.8" 2023-07-18`: 17,
		`java version "1.8.0_381"`:            8,
		`openjdk version "21" 2023-09-19`:     21,
	}
	for in, want := range cases {
		if got, err := javaMajor(in); err != nil || got != want {
			t.Errorf("%s: %d %v", in, got, err)
		}
	}
	if _, err := javaMajor("basura"); err == nil {
		t.Error("salida no reconocida")
	}
}

func TestCheckWritable(t *testing.T) {
	if err := checkWritable(filepath.Join(t.TempDir(), "nueva", "sub")); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "archivo")
	os.WriteFile(blocker, nil, 0o644)
	if err := checkWritable(filepath.Join(blocker, "sub")); err == nil {
		t.Fatal("debía fallar")
	}
}

// Las credenciales no llegan al disco: la sesión solo vive en memoria.
func TestLoginDoesNotPersistTokens(t *testing.T) {
	_, paths := withFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := browserLogin(ctx, "cliente-prueba"); err != nil {
		t.Fatal(err)
	}
	filepath.WalkDir(paths.Root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if data, _ := os.ReadFile(p); strings.Contains(string(data), "refresco-1") || strings.Contains(string(data), "mc-1") {
				t.Errorf("token en disco: %s", p)
			}
		}
		return nil
	})
}

func TestLegacyAuthFileIsDeleted(t *testing.T) {
	paths := testPaths(t)
	legacy := filepath.Join(paths.Root, "auth.json")
	os.WriteFile(legacy, []byte(`{"refresh_token":"x"}`), 0o600)
	if _, err := newApp(paths); err != nil {
		t.Fatal(err)
	}
	if fileExists(legacy) {
		t.Fatal("debe borrar la sesión antigua")
	}
}

// Prueba 16 (la parte sin Minecraft real): los argumentos de la instancia.
func TestLaunchArgsUseInstance(t *testing.T) {
	paths := testPaths(t)
	id := "1.20.1-forge-47.4.10"
	os.MkdirAll(filepath.Join(paths.Versions, id), 0o755)
	os.WriteFile(filepath.Join(paths.Versions, id, id+".json"), []byte(`{"mainClass":"cpw.mods.bootstraplauncher.BootstrapLauncher","arguments":{"game":["--fml.forgeVersion","47.4.10"],"jvm":["-DlibraryDirectory=${library_directory}"]}}`), 0o644)
	os.MkdirAll(filepath.Join(paths.Versions, "1.20.1"), 0o755)
	os.WriteFile(filepath.Join(paths.Versions, "1.20.1", "1.20.1.jar"), []byte("jar"), 0o644)
	var vanilla VersionJSON
	json.Unmarshal([]byte(`{"id":"1.20.1","assetIndex":{"id":"5"},"arguments":{"game":["--gameDir","${game_directory}","--username","${auth_player_name}"],"jvm":["-cp","${classpath}"]}}`), &vanilla)
	args, err := buildLaunchArgs(paths, Config{MemoryMB: 4096, JavaArgs: "-XX:+UseG1GC"}, &Account{Name: "Steve"}, &vanilla, id)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-Xmx4096m", "-XX:+UseG1GC", "cpw.mods.bootstraplauncher.BootstrapLauncher", "--gameDir " + paths.Instance, "--username Steve", "--fml.forgeVersion 47.4.10", "-DlibraryDirectory=" + paths.Libraries} {
		if !strings.Contains(joined, want) {
			t.Errorf("falta %q en %s", want, joined)
		}
	}
}
