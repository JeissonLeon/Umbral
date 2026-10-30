//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
)

// Fuera de Windows la interfaz se abre en el navegador; sirve para
// desarrollar y probar. UMBRAL_NO_BROWSER=1 solo imprime la dirección.
func runWindow(url string, paths Paths) error {
	fmt.Println("Interfaz en", url)
	if os.Getenv("UMBRAL_NO_BROWSER") == "" {
		openPath(url)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	return nil
}

func showError(msg string) { fmt.Fprintln(os.Stderr, "Error:", msg) }

func hideConsole(cmd *exec.Cmd) {}

func openPath(target string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	exec.Command(name, target).Start()
}
