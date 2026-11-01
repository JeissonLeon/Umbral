// Package selfupdate reemplaza el propio launcher. El launcher prepara la
// versión nueva (Stage) y lanza el actualizador, un ejecutable aparte que espera
// a que se cierre, hace el cambio y vuelve atrás si la versión nueva no arranca.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"umbral/internal/pack"
)

const (
	LauncherExe = "UmbralLauncher.exe"
	UpdaterExe  = "umbral-updater.exe"
)

// Compare compara versiones x.y.z: negativo si a<b, 0 si iguales, positivo si a>b.
func Compare(a, b string) int {
	as, bs := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < max(len(as), len(bs)); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

func stagedDir(data string) string    { return filepath.Join(data, "actualizacion-launcher") }
func rejectedFile(data string) string { return filepath.Join(data, "launcher-rechazado.json") }

// Available dice si el manifiesto trae una versión instalable más nueva que la
// actual y que no se haya revertido ya en este equipo.
func Available(m *pack.Manifest, current, data string) bool {
	if len(m.Launcher.Files) == 0 || Compare(m.Launcher.Version, current) <= 0 {
		return false
	}
	var r struct {
		Version string `json:"version"`
	}
	if raw, err := os.ReadFile(rejectedFile(data)); err == nil && json.Unmarshal(raw, &r) == nil && r.Version == m.Launcher.Version {
		return false
	}
	return true
}

// Stage descarga y verifica los ejecutables nuevos, guardando junto a ellos el
// manifiesto firmado para que el actualizador compruebe todo por su cuenta.
func Stage(ctx context.Context, m *pack.Manifest, rawManifest, sig []byte, data string, onBytes func(int64)) (string, error) {
	dir := stagedDir(data)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	found := false
	for _, a := range m.Launcher.Files {
		if err := pack.Fetch(ctx, a.URL, filepath.Join(dir, a.Name), a.SHA256, a.Size, onBytes); err != nil {
			return "", fmt.Errorf("%s: %w", a.Name, err)
		}
		found = found || a.Name == LauncherExe
	}
	if !found {
		return "", fmt.Errorf("el manifiesto no incluye %s", LauncherExe)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), rawManifest, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json.sig"), sig, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// SwapUpdater pone en su sitio el actualizador nuevo. Lo hace el launcher,
// porque un ejecutable no puede reemplazarse a sí mismo mientras corre.
func SwapUpdater(staged, installDir string) error {
	src := filepath.Join(staged, UpdaterExe)
	if _, err := os.Stat(src); err != nil {
		return nil // esta versión no cambia el actualizador
	}
	return copyAtomic(src, filepath.Join(installDir, UpdaterExe))
}

// Process es lo mínimo que el actualizador necesita de la versión nueva.
type Process interface {
	Wait() error
	Kill() error
}

// Options agrupa lo que Run necesita; las pruebas sustituyen Start y WaitParent.
type Options struct {
	InstallDir     string
	Staged         string
	Data           string
	CurrentVersion string
	Keys           []ed25519.PublicKey
	WaitParent     func() error
	Start          func(exe string, args ...string) (Process, error)
	ConfirmTimeout time.Duration
	OnSuccess      func(version string)
}

// ErrRolledBack indica que la versión nueva falló y se restauró la anterior.
var ErrRolledBack = errors.New("la versión nueva no arrancó; se restauró la anterior")

// Run hace el cambio. Termine como termine, deja un launcher en marcha.
func Run(o Options) error {
	exe := filepath.Join(o.InstallDir, LauncherExe)
	restart := func() {
		if _, err := o.Start(exe); err != nil {
			log.Println("No se pudo reabrir el launcher:", err)
		}
	}
	waited := false
	fail := func(err error) error {
		log.Println("Actualización cancelada:", err)
		if !waited {
			o.WaitParent()
		}
		restart()
		return err
	}

	raw, err := os.ReadFile(filepath.Join(o.Staged, "manifest.json"))
	if err != nil {
		return fail(err)
	}
	sig, err := os.ReadFile(filepath.Join(o.Staged, "manifest.json.sig"))
	if err != nil {
		return fail(err)
	}
	m, err := pack.LoadVerified(raw, sig, o.Keys)
	if err != nil {
		return fail(err)
	}
	if Compare(m.Launcher.Version, o.CurrentVersion) <= 0 {
		return fail(fmt.Errorf("la versión %s no es más nueva que %s", m.Launcher.Version, o.CurrentVersion))
	}
	var asset *pack.Asset
	for i := range m.Launcher.Files {
		if m.Launcher.Files[i].Name == LauncherExe {
			asset = &m.Launcher.Files[i]
		}
	}
	if asset == nil {
		return fail(fmt.Errorf("el manifiesto no incluye %s", LauncherExe))
	}
	src := filepath.Join(o.Staged, LauncherExe)
	if h, err := pack.HashFile(src); err != nil || h != asset.SHA256 {
		return fail(fmt.Errorf("%w: %s", pack.ErrCorrupt, LauncherExe))
	}

	if err := o.WaitParent(); err != nil {
		waited = true
		return fail(fmt.Errorf("el launcher no se cerró: %w", err))
	}
	waited = true

	backup := exe + ".bak"
	if err := copyAtomic(exe, backup); err != nil {
		return fail(fmt.Errorf("no se pudo respaldar: %w", err))
	}
	// El antivirus puede retener el archivo unos segundos tras cerrarse.
	var swapErr error
	for i := 0; i < 20; i++ {
		if swapErr = copyAtomic(src, exe); swapErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if swapErr != nil {
		return fail(fmt.Errorf("no se pudo reemplazar: %w", swapErr))
	}

	marker := filepath.Join(o.Data, "launcher-confirmado-"+m.Launcher.Version)
	os.Remove(marker)
	ok := false
	if proc, err := o.Start(exe, "--confirm-update", marker); err != nil {
		log.Println("La versión nueva no se pudo abrir:", err)
	} else {
		ok = waitConfirmation(proc, marker, o.ConfirmTimeout)
	}
	if ok {
		os.Remove(marker)
		os.Remove(backup)
		os.RemoveAll(o.Staged)
		os.Remove(rejectedFile(o.Data))
		if o.OnSuccess != nil {
			o.OnSuccess(m.Launcher.Version)
		}
		log.Println("Launcher actualizado a", m.Launcher.Version)
		return nil
	}

	if err := os.Rename(backup, exe); err != nil {
		log.Println("No se pudo restaurar el launcher:", err)
		return fmt.Errorf("no se pudo restaurar la versión anterior: %w", err)
	}
	if data, err := json.Marshal(map[string]string{"version": m.Launcher.Version}); err == nil {
		os.WriteFile(rejectedFile(o.Data), data, 0o644)
	}
	os.RemoveAll(o.Staged)
	restart()
	return ErrRolledBack
}

// waitConfirmation espera a que el proceso nuevo cree el marcador.
func waitConfirmation(proc Process, marker string, timeout time.Duration) bool {
	exited := make(chan struct{})
	go func() { proc.Wait(); close(exited) }()
	deadline := time.After(timeout)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if _, err := os.Stat(marker); err == nil {
				return true
			}
		case <-exited:
			_, err := os.Stat(marker)
			return err == nil
		case <-deadline:
			proc.Kill()
			return false
		}
	}
}

// Confirm lo llama la versión nueva cuando su ventana ya está lista.
func Confirm(marker string) error {
	return os.WriteFile(marker, []byte("ok"), 0o644)
}

// CleanLeftovers borra los restos de una actualización ya terminada.
func CleanLeftovers(installDir string) { os.Remove(filepath.Join(installDir, LauncherExe+".bak")) }

func copyAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
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
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
