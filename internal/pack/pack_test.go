package pack

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func init() {
	RetryDelay = time.Millisecond
	StallTimeout = 2 * time.Second
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// cdn sirve contenidos en /<nombre>, con Range, como GitHub Releases.
type cdn struct {
	*httptest.Server
	files map[string]string
	hits  map[string]*atomic.Int64
	// cutAfter corta la conexión tras N bytes en la primera petición.
	cutAfter int
	cut      atomic.Bool
	// offline hace que todas las conexiones se corten.
	offline atomic.Bool
}

func newCDN(t *testing.T) *cdn {
	c := &cdn{files: map[string]string{}, hits: map[string]*atomic.Int64{}}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if c.offline.Load() {
			panic(http.ErrAbortHandler)
		}
		body, ok := c.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if c.hits[name] == nil {
			c.hits[name] = &atomic.Int64{}
		}
		c.hits[name].Add(1)
		start := 0
		if rg := r.Header.Get("Range"); rg != "" {
			start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)-start))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		if c.cutAfter > 0 && !c.cut.Swap(true) {
			w.Write([]byte(body[start : start+c.cutAfter]))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		w.Write([]byte(body[start:]))
	}))
	t.Cleanup(c.Close)
	return c
}

func (c *cdn) file(path, content string) File {
	name := hash(content)[:12]
	c.files[name] = content
	return File{Path: path, Version: "1", URL: c.URL + "/" + name, SHA256: hash(content), Size: int64(len(content)), Required: true}
}

func manifestOf(seq int64, files []File, remove ...Remove) *Manifest {
	return &Manifest{
		SchemaVersion: 1, Sequence: seq,
		Modpack:  Modpack{ID: "sylvanor3", Version: fmt.Sprint(seq), Minecraft: "1.20.1", Forge: "47.4.10", Java: 17},
		Launcher: Launcher{Version: "1.0.0"},
		Files:    files, Remove: remove,
	}
}

func newInstaller(t *testing.T) *Installer {
	dir := t.TempDir()
	return &Installer{Root: filepath.Join(dir, "instancia"), Dir: filepath.Join(dir, "estado")}
}

func sync_(t *testing.T, in *Installer, m *Manifest) *Plan {
	t.Helper()
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	plan, err := in.Plan(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Apply(context.Background(), m, plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func read(t *testing.T, in *Installer, p string) string {
	t.Helper()
	data, err := os.ReadFile(in.local(p))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(in *Installer, p string) bool {
	_, err := os.Stat(in.local(p))
	return err == nil
}

// Prueba 1: instalación desde cero. Prueba 2 y 3: descarga y SHA-256.
func TestInstallFromScratch(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	m := manifestOf(1, []File{c.file("mods/a.jar", "mod a"), c.file("config/a.toml", "cfg")})
	plan := sync_(t, in, m)
	if len(plan.Add) != 2 {
		t.Fatalf("plan: %+v", plan)
	}
	if read(t, in, "mods/a.jar") != "mod a" || read(t, in, "config/a.toml") != "cfg" {
		t.Fatal("contenido incorrecto")
	}
	st, _ := in.LoadState()
	if st.Sequence != 1 || len(st.Files) != 2 {
		t.Fatalf("registro: %+v", st)
	}
}

// Incremental: lo que no cambió no se descarga otra vez.
func TestIncrementalDownloadsOnlyChanges(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	same := c.file("mods/igual.jar", "igual")
	sync_(t, in, manifestOf(1, []File{same, c.file("mods/b.jar", "v1")}))
	plan := sync_(t, in, manifestOf(2, []File{same, c.file("mods/b.jar", "v2")}))
	if len(plan.Update) != 1 || plan.Current != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	if got := c.hits[hash("igual")[:12]].Load(); got != 1 {
		t.Fatalf("el archivo sin cambios se descargó %d veces", got)
	}
}

// Prueba 4: archivo corrupto en el servidor. No se instala y se conserva el anterior.
func TestCorruptDownloadRejected(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	sync_(t, in, manifestOf(1, []File{c.file("mods/a.jar", "bueno")}))

	f := c.file("mods/a.jar", "nuevo")
	c.files[hash("nuevo")[:12]] = "manipulado"
	f.Size = int64(len("manipulado"))
	m := manifestOf(2, []File{f})
	plan, err := in.Plan(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Apply(context.Background(), m, plan); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("se esperaba ErrCorrupt, llegó %v", err)
	}
	if read(t, in, "mods/a.jar") != "bueno" {
		t.Fatal("se tocó el archivo instalado")
	}
	if st, _ := in.LoadState(); st.Sequence != 1 {
		t.Fatal("no debe constar como actualizado")
	}
}

// Prueba 5: archivo ausente en disco o en el servidor.
func TestMissingFile(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	a := c.file("mods/a.jar", "a")
	m := manifestOf(1, []File{a})
	sync_(t, in, m)
	os.Remove(in.local("mods/a.jar"))
	if plan := sync_(t, in, m); len(plan.Add) != 1 {
		t.Fatalf("no se restauró: %+v", plan)
	}

	gone := File{Path: "mods/x.jar", Version: "1", URL: c.URL + "/noexiste", SHA256: hash("x"), Size: 1, Required: true}
	m2 := manifestOf(2, []File{a, gone})
	plan, _ := in.Plan(m2)
	if err := in.Apply(context.Background(), m2, plan); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("error esperado 404: %v", err)
	}
	if st, _ := in.LoadState(); st.Sequence != 1 {
		t.Fatal("no debe constar como actualizado si falta un obligatorio")
	}
}

// Un opcional que falla no bloquea la actualización.
func TestOptionalFailureDoesNotBlock(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	in.Enabled = func(string) bool { return true }
	m := manifestOf(1, []File{
		c.file("mods/a.jar", "a"),
		{Path: "mods/op.jar", Version: "1", URL: c.URL + "/noexiste", SHA256: hash("op"), Size: 2},
	})
	sync_(t, in, m)
	if !exists(in, "mods/a.jar") || exists(in, "mods/op.jar") {
		t.Fatal("estado inesperado")
	}
}

// Pruebas 6, 7, 8 y 9: actualizar, añadir y eliminar mods; actualizar config.
func TestReconcileAddUpdateRemove(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	sync_(t, in, manifestOf(1, []File{c.file("mods/a.jar", "a1"), c.file("mods/viejo.jar", "v"), c.file("config/a.toml", "c1")}))
	// Archivo del jugador: el launcher no lo registró, nunca debe tocarlo.
	os.WriteFile(in.local("config/mio.toml"), []byte("personal"), 0o644)

	plan := sync_(t, in, manifestOf(2, []File{c.file("mods/a.jar", "a2"), c.file("mods/nuevo.jar", "n"), c.file("config/a.toml", "c2")}))
	if len(plan.Add) != 1 || len(plan.Update) != 2 || len(plan.Delete) != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	if read(t, in, "mods/a.jar") != "a2" || read(t, in, "mods/nuevo.jar") != "n" || read(t, in, "config/a.toml") != "c2" {
		t.Fatal("no se aplicó")
	}
	if exists(in, "mods/viejo.jar") {
		t.Fatal("el mod retirado sigue ahí")
	}
	if read(t, in, "config/mio.toml") != "personal" {
		t.Fatal("se tocó un archivo personal")
	}
}

func TestRemoveListRespectsOwnership(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	os.MkdirAll(in.local("mods"), 0o755)
	os.WriteFile(in.local("mods/legado.jar"), []byte("legado"), 0o644)
	os.WriteFile(in.local("mods/delJugador.jar"), []byte("mio"), 0o644)
	m := manifestOf(1, []File{c.file("mods/a.jar", "a")},
		Remove{Path: "mods/legado.jar", SHA256: hash("legado")},
		Remove{Path: "mods/delJugador.jar", SHA256: hash("otro contenido")},
		Remove{Path: "mods/sinHash.jar"})
	os.WriteFile(in.local("mods/sinHash.jar"), []byte("?"), 0o644)
	sync_(t, in, m)
	if exists(in, "mods/legado.jar") {
		t.Fatal("el legado con hash coincidente debe eliminarse")
	}
	if !exists(in, "mods/delJugador.jar") || !exists(in, "mods/sinHash.jar") {
		t.Fatal("se borró un archivo no administrado")
	}
}

func TestRestoresModifiedManagedFile(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	m := manifestOf(1, []File{c.file("mods/a.jar", "original")})
	sync_(t, in, m)
	os.WriteFile(in.local("mods/a.jar"), []byte("alterado"), 0o644)
	if plan := sync_(t, in, m); len(plan.Update) != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	if read(t, in, "mods/a.jar") != "original" {
		t.Fatal("no se restauró")
	}
}

func TestSeedConfigKeepsPlayerEditsUntilManifestChanges(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	f := c.file("config/teclas.toml", "v1")
	f.Policy = PolicySeed
	m := manifestOf(1, []File{f})
	sync_(t, in, m)
	os.WriteFile(in.local("config/teclas.toml"), []byte("del jugador"), 0o644)
	if plan := sync_(t, in, m); !plan.Empty() {
		t.Fatalf("debe respetar la edición: %+v", plan)
	}
	g := c.file("config/teclas.toml", "v2")
	g.Policy = PolicySeed
	sync_(t, in, manifestOf(2, []File{g}))
	if read(t, in, "config/teclas.toml") != "v2" {
		t.Fatal("la definición nueva debe prevalecer")
	}
}

func TestDisablingOptionalRemovesIt(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	on := true
	in.Enabled = func(string) bool { return on }
	op := c.file("mods/op.jar", "op")
	op.Required = false
	m := manifestOf(1, []File{c.file("mods/a.jar", "a"), op})
	sync_(t, in, m)
	if !exists(in, "mods/op.jar") {
		t.Fatal("no se instaló")
	}
	on = false
	sync_(t, in, m)
	if exists(in, "mods/op.jar") {
		t.Fatal("no se quitó")
	}
}

// Prueba 11: la descarga se corta y continúa desde donde iba.
func TestInterruptedDownloadResumes(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	content := strings.Repeat("x", 300_000)
	c.cutAfter = 100_000
	sync_(t, in, manifestOf(1, []File{c.file("mods/grande.jar", content)}))
	if read(t, in, "mods/grande.jar") != content {
		t.Fatal("contenido incorrecto")
	}
	if got := c.hits[hash(content)[:12]].Load(); got != 2 {
		t.Fatalf("se esperaban 2 peticiones (corte y continuación), hubo %d", got)
	}
}

// Prueba 12: sin conexión no se toca nada y se puede reintentar.
func TestConnectionLoss(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	sync_(t, in, manifestOf(1, []File{c.file("mods/a.jar", "a1")}))
	m := manifestOf(2, []File{c.file("mods/a.jar", "a2")})
	plan, _ := in.Plan(m)
	c.offline.Store(true)
	if err := in.Apply(context.Background(), m, plan); err == nil {
		t.Fatal("debía fallar")
	}
	if read(t, in, "mods/a.jar") != "a1" {
		t.Fatal("se tocó la instalación")
	}
	c.offline.Store(false)
	sync_(t, in, m)
	if read(t, in, "mods/a.jar") != "a2" {
		t.Fatal("el reintento no funcionó")
	}
}

func TestCancelStopsDownload(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	m := manifestOf(1, []File{c.file("mods/a.jar", "a")})
	plan, _ := in.Plan(m)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := in.Apply(ctx, m, plan); !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
	if exists(in, "mods/a.jar") {
		t.Fatal("no debe instalar nada")
	}
}

// Cierre inesperado a mitad de aplicar: al arrancar se deshace.
func TestRecoverRollsBackInterruptedTransaction(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	sync_(t, in, manifestOf(1, []File{c.file("mods/a.jar", "a1"), c.file("mods/b.jar", "b1")}))

	// Simula el estado tras el corte: a.jar reemplazado, b.jar retirado, nuevo.jar añadido.
	backup := in.backupDir()
	os.MkdirAll(backup, 0o755)
	os.Rename(in.local("mods/a.jar"), filepath.Join(backup, "1"))
	os.WriteFile(in.local("mods/a.jar"), []byte("a2"), 0o644)
	os.Rename(in.local("mods/b.jar"), filepath.Join(backup, "2"))
	os.WriteFile(in.local("mods/nuevo.jar"), []byte("n"), 0o644)
	if err := writeJSONAtomic(in.journalPath(), journal{TxID: "2-x", Ops: []txOp{
		{Path: "mods/a.jar", Backup: filepath.Join(backup, "1")},
		{Path: "mods/b.jar", Backup: filepath.Join(backup, "2")},
		{Path: "mods/nuevo.jar"},
		{Path: "mods/sinAplicar.jar", Backup: filepath.Join(backup, "3")},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := in.Recover(); err != nil {
		t.Fatal(err)
	}
	if read(t, in, "mods/a.jar") != "a1" || read(t, in, "mods/b.jar") != "b1" || exists(in, "mods/nuevo.jar") {
		t.Fatal("no se deshizo")
	}
	if _, err := os.Stat(in.journalPath()); err == nil {
		t.Fatal("la transacción debe desaparecer")
	}
}

// Si el corte fue tras escribir el registro, la actualización vale y no se deshace.
func TestRecoverKeepsCommittedTransaction(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	sync_(t, in, manifestOf(1, []File{c.file("mods/a.jar", "a1")}))
	st, _ := in.LoadState()
	writeJSONAtomic(in.journalPath(), journal{TxID: st.TxID, Ops: []txOp{{Path: "mods/a.jar"}}})
	if err := in.Recover(); err != nil {
		t.Fatal(err)
	}
	if read(t, in, "mods/a.jar") != "a1" {
		t.Fatal("deshizo una transacción ya confirmada")
	}
}

// Prueba 13: manifiesto inválido.
func TestInvalidManifests(t *testing.T) {
	good := func() *Manifest {
		return manifestOf(1, []File{{Path: "mods/a.jar", Version: "1", URL: "https://x.test/a", SHA256: hash("a"), Size: 1, Required: true}})
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Manifest){
		"esquema futuro":    func(m *Manifest) { m.SchemaVersion = 2 },
		"sin secuencia":     func(m *Manifest) { m.Sequence = 0 },
		"sin forge":         func(m *Manifest) { m.Modpack.Forge = "" },
		"sha corto":         func(m *Manifest) { m.Files[0].SHA256 = "abc" },
		"sin tamaño":        func(m *Manifest) { m.Files[0].Size = 0 },
		"http":              func(m *Manifest) { m.Files[0].URL = "http://x.test/a" },
		"salida de carpeta": func(m *Manifest) { m.Files[0].Path = "mods/../../a.jar" },
		"mundos":            func(m *Manifest) { m.Files[0].Path = "saves/mundo/level.dat" },
		"duplicado":         func(m *Manifest) { m.Files = append(m.Files, m.Files[0]) },
		"mayúsculas dup": func(m *Manifest) {
			f := m.Files[0]
			f.Path = "mods/A.JAR"
			m.Files = append(m.Files, f)
		},
		"a la vez":     func(m *Manifest) { m.Remove = []Remove{{Path: "mods/a.jar"}} },
		"remove fuera": func(m *Manifest) { m.Remove = []Remove{{Path: "../x"}} },
		"umbral falta": func(m *Manifest) { m.UmbralMod.Path = "mods/otro.jar" },
		"exe inseguro": func(m *Manifest) {
			m.Launcher.Files = []Asset{{Name: "..\\x.exe", URL: "https://x.test/a", SHA256: hash("a"), Size: 1}}
		},
	}
	for name, mutate := range cases {
		m := good()
		mutate(m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: debía rechazarse", name)
		}
	}
	if _, err := Parse([]byte("{ roto")); err == nil {
		t.Error("JSON roto aceptado")
	}
}

func TestSafePath(t *testing.T) {
	for _, bad := range []string{"", "a.jar", "/mods/a.jar", "C:/mods/a.jar", `mods\a.jar`, "mods/../a", "./mods/a", "mods/a.jar:ads", "mods/con.jar", "mods/NUL", "mods/a.", "mods/a ", "saves/a", "../mods/a", "mods//a"} {
		if SafePath(bad) == nil {
			t.Errorf("%q debía rechazarse", bad)
		}
	}
	for _, ok := range []string{"mods/a.jar", "config/x/y.toml", "resourcepacks/Pack Ω.zip", "mods/console.jar"} {
		if err := SafePath(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

// Prueba 14: firma incorrecta.
func TestSignature(t *testing.T) {
	pub, priv, _ := GenerateKey()
	keys, err := ParseKeys("# comentario\n\n" + pub + "\n")
	if err != nil || len(keys) != 1 {
		t.Fatal(keys, err)
	}
	sk, _ := ParsePrivateKey(priv)
	m := manifestOf(1, nil)
	data, _ := json.Marshal(m)
	sig, err := Sign(data, sk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadVerified(data, sig, keys); err != nil {
		t.Fatalf("firma válida rechazada: %v", err)
	}
	tampered := []byte(strings.Replace(string(data), "1.20.1", "1.20.2", 1))
	if _, err := LoadVerified(tampered, sig, keys); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("manifiesto alterado aceptado: %v", err)
	}
	_, other, _ := ed25519.GenerateKey(nil)
	bad, _ := Sign(data, other)
	if _, err := LoadVerified(data, bad, keys); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("clave ajena aceptada: %v", err)
	}
	if _, err := LoadVerified(data, []byte("basura"), keys); !errors.Is(err, ErrBadSignature) {
		t.Fatal("firma basura aceptada")
	}
	if err := Verify(data, sig, nil); !errors.Is(err, ErrBadSignature) {
		t.Fatal("sin claves no se puede aceptar nada")
	}
}

// Rotación: un launcher con la clave vieja y la nueva acepta ambas.
func TestKeyRotation(t *testing.T) {
	oldPub, oldPriv, _ := GenerateKey()
	newPub, newPriv, _ := GenerateKey()
	data := []byte("{}")
	both, _ := ParseKeys(oldPub + "\n" + newPub)
	onlyOld, _ := ParseKeys(oldPub)
	for _, p := range []string{oldPriv, newPriv} {
		k, _ := ParsePrivateKey(p)
		sig, _ := Sign(data, k)
		if err := Verify(data, sig, both); err != nil {
			t.Fatal(err)
		}
	}
	k, _ := ParsePrivateKey(newPriv)
	sig, _ := Sign(data, k)
	if Verify(data, sig, onlyOld) == nil {
		t.Fatal("la clave nueva no debe valer en un launcher antiguo")
	}
}

// Prueba 15: directorio sin permisos. Se simula con un archivo donde debería
// ir la carpeta, que falla igual en cualquier sistema y cuenta de usuario.
func TestUnwritableInstallDir(t *testing.T) {
	c := newCDN(t)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "bloqueo")
	os.WriteFile(blocker, nil, 0o644)
	in := &Installer{Root: filepath.Join(blocker, "instancia"), Dir: filepath.Join(dir, "estado")}
	m := manifestOf(1, []File{c.file("mods/a.jar", "a")})
	plan := &Plan{Add: m.Files}
	if err := in.Apply(context.Background(), m, plan); err == nil {
		t.Fatal("debía fallar")
	}
	if st, _ := in.LoadState(); st.Sequence != 0 {
		t.Fatal("no debe constar como instalado")
	}
}

// Fallo al aplicar con un archivo bloqueado: se deshace todo.
func TestApplyFailureRollsBack(t *testing.T) {
	c, in := newCDN(t), newInstaller(t)
	sync_(t, in, manifestOf(1, []File{c.file("mods/a.jar", "a1")}))
	// mods/x es un archivo, así que mods/x/b.jar no se puede crear: el primer
	// reemplazo ya se hizo cuando falla el segundo.
	os.WriteFile(in.local("mods/x"), []byte("x"), 0o644)
	m := manifestOf(2, []File{c.file("mods/a.jar", "a2"), c.file("mods/x/b.jar", "b")})
	plan := &Plan{Update: m.Files}
	if err := in.Apply(context.Background(), m, plan); err == nil {
		t.Fatal("debía fallar")
	}
	if read(t, in, "mods/a.jar") != "a1" {
		t.Fatal("no se deshizo")
	}
	if st, _ := in.LoadState(); st.Sequence != 1 {
		t.Fatal("el registro no debe avanzar")
	}
}
