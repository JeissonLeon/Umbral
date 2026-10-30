// Umbral Launcher: instala Minecraft 1.20.1 con Forge y el modpack, inicia
// sesión con Microsoft y abre el juego.
//
// La interfaz es una página web embebida que se sirve en 127.0.0.1 y se
// muestra en una ventana WebView2 (Windows). En otros sistemas se abre en
// el navegador, lo que sirve para desarrollar y probar.
package main

import (
	"embed"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// Se fijan al compilar con -ldflags "-X main.defaultClientID=...".
var (
	launcherVersion    = "0.2.0"
	defaultClientID    = ""
	defaultManifestURL = ""
)

//go:embed manifiesto.json
var manifestJSON []byte

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

func newPaths() (Paths, error) {
	root := os.Getenv("UMBRAL_HOME")
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(base, "umbral")
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
	if err := runWindow(url, paths); err != nil {
		showError(fmt.Sprintf("No se pudo abrir la ventana del launcher:\n%v", err))
		os.Exit(1)
	}
}
