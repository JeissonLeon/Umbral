// Umbral Launcher: instala y mantiene SylvanΩr 3 (Minecraft, Forge y modpack)
// a partir de un manifiesto firmado en GitHub, y abre la instancia preparada.
//
// La interfaz es una página web embebida que se sirve en 127.0.0.1 y se
// muestra en una ventana WebView2 (Windows). En otros sistemas se abre en
// el navegador, lo que sirve para desarrollar y probar.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"umbral/internal/selfupdate"
)

// Se fijan al compilar con -ldflags "-X main.defaultClientID=...".
var (
	launcherVersion    = "1.0.0"
	defaultClientID    = ""
	defaultManifestURL = ""
)

//go:embed ui
var uiFiles embed.FS

// Paths agrupa las carpetas de datos del launcher.
type Paths struct {
	Root      string
	Versions  string
	Libraries string
	Assets    string
	Runtime   string
	Cache     string
	Instance  string
}

// locationFile guarda la carpeta de datos elegida en Ajustes. Vive siempre en
// el sitio por defecto, para encontrarla al arrancar.
func locationFile() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "umbral", "ubicacion.txt"), nil
}

func setDataLocation(dir string) error {
	p, err := locationFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(dir), 0o644)
}

// checkWritable crea la carpeta y comprueba que se pueda escribir en ella.
func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("no se pudo crear %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".umbral-prueba-*")
	if err != nil {
		return fmt.Errorf("sin permiso de escritura en %s: %w", dir, err)
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

func newPaths() (Paths, error) {
	root := os.Getenv("UMBRAL_HOME")
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(base, "umbral")
		if loc, err := locationFile(); err == nil {
			if data, err := os.ReadFile(loc); err == nil && len(data) > 0 {
				root = strings.TrimSpace(string(data))
			}
		}
	}
	p := Paths{
		Root:      root,
		Versions:  filepath.Join(root, "versions"),
		Libraries: filepath.Join(root, "libraries"),
		Assets:    filepath.Join(root, "assets"),
		Runtime:   filepath.Join(root, "runtime"),
		Cache:     filepath.Join(root, "cache"),
		Instance:  filepath.Join(root, "instancia"),
	}
	for _, d := range []string{p.Versions, p.Libraries, p.Assets, p.Runtime, p.Cache, p.Instance} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return Paths{}, err
		}
	}
	return p, nil
}

// setupLog escribe el registro del launcher en launcher.log, empezando de
// cero en cada arranque para que no crezca sin límite.
func setupLog(paths Paths) {
	f, err := os.Create(filepath.Join(paths.Root, "launcher.log"))
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.SetFlags(log.Ldate | log.Ltime)
}

func main() {
	confirm := flag.String("confirm-update", "", "marcador que confirma al actualizador que esta versión arrancó")
	flag.Parse()

	paths, err := newPaths()
	if err != nil {
		showError("No se pudo crear la carpeta de datos del launcher:\n" + err.Error())
		os.Exit(1)
	}
	setupLog(paths)
	log.Printf("Umbral Launcher %s, datos en %s", launcherVersion, paths.Root)

	app, err := newApp(paths)
	if err != nil {
		showError(err.Error())
		os.Exit(1)
	}
	url, err := app.serve()
	if err != nil {
		showError("No se pudo iniciar la interfaz:\n" + err.Error())
		os.Exit(1)
	}
	if exe, err := os.Executable(); err == nil && *confirm == "" {
		selfupdate.CleanLeftovers(filepath.Dir(exe))
	}
	onReady := func() {
		if *confirm != "" {
			if err := selfupdate.Confirm(*confirm); err != nil {
				log.Println("No se pudo confirmar la actualización:", err)
			}
		}
	}
	if err := runWindow(url, paths, app, onReady); err != nil {
		showError(fmt.Sprintf("No se pudo abrir la ventana del launcher:\n%v", err))
		os.Exit(1)
	}
}
