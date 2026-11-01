package selfupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"umbral/internal/pack"
)

type fakeProc struct{ done chan struct{} }

func (p *fakeProc) Wait() error { <-p.done; return nil }
func (p *fakeProc) Kill() error { close(p.done); return nil }

type env struct {
	opts    Options
	keys    []ed25519.PublicKey
	priv    ed25519.PrivateKey
	starts  [][]string
	onStart func(args []string) // simula lo que hace el proceso abierto
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// setup prepara una instalación 1.0.0 y una versión 2.0.0 ya descargada.
func setup(t *testing.T, content string) *env {
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(nil)
	e := &env{keys: []ed25519.PublicKey{pub}, priv: priv}
	install, data, staged := filepath.Join(dir, "app"), filepath.Join(dir, "datos"), filepath.Join(dir, "nueva")
	for _, d := range []string{install, data, staged} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(install, LauncherExe), []byte("viejo"), 0o644)
	os.WriteFile(filepath.Join(staged, LauncherExe), []byte(content), 0o644)
	src := filepath.Join(staged, LauncherExe)
	h, _ := pack.HashFile(src)
	e.sign(t, staged, "2.0.0", h, int64(len(content)))
	e.opts = Options{
		InstallDir: install, Staged: staged, Data: data, CurrentVersion: "1.0.0", Keys: e.keys,
		WaitParent:     func() error { return nil },
		ConfirmTimeout: 2 * time.Second,
		Start: func(exe string, args ...string) (Process, error) {
			e.starts = append(e.starts, append([]string{read(t, exe)}, args...))
			p := &fakeProc{done: make(chan struct{})}
			if e.onStart != nil {
				go func() { e.onStart(args); close(p.done) }()
			} else {
				close(p.done)
			}
			return p, nil
		},
	}
	return e
}

func (e *env) sign(t *testing.T, staged, version, sha string, size int64) {
	m := pack.Manifest{SchemaVersion: 1, Sequence: 1,
		Modpack:  pack.Modpack{ID: "s", Version: "1", Minecraft: "1.20.1", Forge: "47.4.10", Java: 17},
		Launcher: pack.Launcher{Version: version, Files: []pack.Asset{{Name: LauncherExe, URL: "https://x.test/l", SHA256: sha, Size: size}}},
	}
	raw, _ := json.Marshal(m)
	sig, err := pack.Sign(raw, e.priv)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(staged, "manifest.json"), raw, 0o644)
	os.WriteFile(filepath.Join(staged, "manifest.json.sig"), sig, 0o644)
}

func (e *env) exe() string { return filepath.Join(e.opts.InstallDir, LauncherExe) }

func confirmOK(args []string) {
	if len(args) == 2 {
		Confirm(args[1])
	}
}

// Prueba 10: actualización del propio launcher.
func TestUpdateSucceeds(t *testing.T) {
	e := setup(t, "nuevo")
	e.onStart = confirmOK
	var reported string
	e.opts.OnSuccess = func(v string) { reported = v }
	if err := Run(e.opts); err != nil {
		t.Fatal(err)
	}
	if read(t, e.exe()) != "nuevo" || reported != "2.0.0" {
		t.Fatal("no se actualizó")
	}
	if _, err := os.Stat(e.exe() + ".bak"); err == nil {
		t.Fatal("quedó el respaldo")
	}
}

func TestRollbackWhenNewVersionExitsWithoutConfirming(t *testing.T) {
	e := setup(t, "roto")
	if err := Run(e.opts); !errors.Is(err, ErrRolledBack) {
		t.Fatalf("error: %v", err)
	}
	if read(t, e.exe()) != "viejo" {
		t.Fatal("no se restauró")
	}
	if last := e.starts[len(e.starts)-1]; last[0] != "viejo" {
		t.Fatalf("debe reabrir la versión vieja: %v", e.starts)
	}
	m := &pack.Manifest{Launcher: pack.Launcher{Version: "2.0.0", Files: []pack.Asset{{}}}}
	if Available(m, "1.0.0", e.opts.Data) {
		t.Fatal("una versión revertida no debe volver a ofrecerse")
	}
	m.Launcher.Version = "2.0.1"
	if !Available(m, "1.0.0", e.opts.Data) {
		t.Fatal("la siguiente versión sí debe ofrecerse")
	}
}

func TestRollbackWhenNewVersionHangs(t *testing.T) {
	e := setup(t, "colgado")
	e.opts.ConfirmTimeout = 200 * time.Millisecond
	e.onStart = func([]string) { time.Sleep(time.Hour) }
	e.opts.Start = func(exe string, args ...string) (Process, error) {
		return &fakeProc{done: make(chan struct{})}, nil
	}
	if err := Run(e.opts); !errors.Is(err, ErrRolledBack) {
		t.Fatalf("error: %v", err)
	}
	if read(t, e.exe()) != "viejo" {
		t.Fatal("no se restauró")
	}
}

func TestRejectsTamperedExecutable(t *testing.T) {
	e := setup(t, "nuevo")
	os.WriteFile(filepath.Join(e.opts.Staged, LauncherExe), []byte("malicioso"), 0o644)
	if err := Run(e.opts); !errors.Is(err, pack.ErrCorrupt) {
		t.Fatalf("error: %v", err)
	}
	if read(t, e.exe()) != "viejo" {
		t.Fatal("se instaló un ejecutable alterado")
	}
	if len(e.starts) != 1 || e.starts[0][0] != "viejo" {
		t.Fatalf("debe reabrir el launcher actual: %v", e.starts)
	}
}

func TestRejectsBadSignature(t *testing.T) {
	e := setup(t, "nuevo")
	other, _, _ := ed25519.GenerateKey(nil)
	e.opts.Keys = []ed25519.PublicKey{other}
	if err := Run(e.opts); !errors.Is(err, pack.ErrBadSignature) {
		t.Fatalf("error: %v", err)
	}
	if read(t, e.exe()) != "viejo" {
		t.Fatal("se instaló sin firma válida")
	}
}

func TestRejectsDowngrade(t *testing.T) {
	e := setup(t, "nuevo")
	e.opts.CurrentVersion = "3.0.0"
	if err := Run(e.opts); err == nil {
		t.Fatal("no debe bajar de versión")
	}
	if read(t, e.exe()) != "viejo" {
		t.Fatal("cambió el ejecutable")
	}
}

func TestParentNeverExits(t *testing.T) {
	e := setup(t, "nuevo")
	e.opts.WaitParent = func() error { return errors.New("sigue abierto") }
	if err := Run(e.opts); err == nil {
		t.Fatal("debía fallar")
	}
	if read(t, e.exe()) != "viejo" {
		t.Fatal("cambió el ejecutable")
	}
}

func TestStageDownloadsAndSwapsUpdater(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.URL.Path[1:])) }))
	defer srv.Close()
	data, install := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(install, UpdaterExe), []byte("viejo"), 0o644)
	asset := func(name, body string) pack.Asset {
		p := filepath.Join(t.TempDir(), "x")
		os.WriteFile(p, []byte(body), 0o644)
		h, _ := pack.HashFile(p)
		return pack.Asset{Name: name, URL: srv.URL + "/" + body, SHA256: h, Size: int64(len(body))}
	}
	m := &pack.Manifest{Launcher: pack.Launcher{Version: "2.0.0", Files: []pack.Asset{asset(LauncherExe, "exe"), asset(UpdaterExe, "upd")}}}
	dir, err := Stage(context.Background(), m, []byte("{}"), []byte("{}"), data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := SwapUpdater(dir, install); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(install, UpdaterExe)) != "upd" {
		t.Fatal("el actualizador no cambió")
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.0.0", "1.0.0", 0}, {"1.2.0", "1.10.0", -1}, {"2.0", "1.9.9", 1}, {"v1.0.1", "1.0", 1}} {
		got := Compare(c.a, c.b)
		if (got < 0) != (c.want < 0) || (got > 0) != (c.want > 0) {
			t.Errorf("%s vs %s = %d", c.a, c.b, got)
		}
	}
}
