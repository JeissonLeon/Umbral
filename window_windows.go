//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

const webview2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

func runWindow(url string, paths Paths, app *App, onReady func()) error {
	// Para desarrollar: sirve la interfaz sin ventana.
	if os.Getenv("UMBRAL_NO_WINDOW") != "" {
		fmt.Println("Interfaz en", url)
		onReady()
		select {}
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(paths.Root, "webview"),
		WindowOptions: webview2.WindowOptions{
			Title:  "Umbral",
			Width:  1120,
			Height: 720,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		if ask("Umbral necesita Microsoft Edge WebView2, que no está instalado en este equipo.\n\n¿Abrir la página de descarga de Microsoft?") {
			openPath(webview2Download)
		}
		return errors.New("falta Microsoft Edge WebView2")
	}
	defer w.Destroy()
	w.SetSize(900, 600, webview2.HintMin)
	w.Navigate(url)
	app.mu.Lock()
	app.closeWindow = func() { w.Dispatch(w.Terminate) }
	app.mu.Unlock()
	onReady()
	w.Run()
	return nil
}

func showError(msg string) {
	text, _ := windows.UTF16PtrFromString(msg)
	title, _ := windows.UTF16PtrFromString("Umbral")
	windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
}

func ask(msg string) bool {
	text, _ := windows.UTF16PtrFromString(msg)
	title, _ := windows.UTF16PtrFromString("Umbral")
	r, _ := windows.MessageBox(0, text, title, windows.MB_YESNO|windows.MB_ICONWARNING)
	return r == 6 // IDYES
}

// hideConsole evita que procesos de consola (instalador de Forge) abran
// una ventana negra.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func openPath(target string) {
	if fi, err := os.Stat(target); err == nil && fi.IsDir() {
		exec.Command("explorer", target).Start()
		return
	}
	exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
}
