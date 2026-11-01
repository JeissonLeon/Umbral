// umbral-updater reemplaza UmbralLauncher.exe, que no puede sobrescribirse
// mientras está abierto. Lo lanza el launcher justo antes de cerrarse.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"umbral/internal/pack"
	"umbral/internal/selfupdate"
)

func main() {
	pid := flag.Int("pid", 0, "proceso del launcher a esperar")
	install := flag.String("install", "", "carpeta de instalación")
	staged := flag.String("staged", "", "carpeta con la versión nueva")
	data := flag.String("data", "", "carpeta de datos")
	current := flag.String("current", "", "versión instalada")
	flag.Parse()
	if *pid == 0 || *install == "" || *staged == "" || *data == "" || *current == "" {
		fmt.Fprintln(os.Stderr, "umbral-updater lo ejecuta el launcher; no se usa directamente")
		os.Exit(2)
	}
	if f, err := os.OpenFile(filepath.Join(*data, "updater.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	keys, err := pack.TrustedKeys()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	err = selfupdate.Run(selfupdate.Options{
		InstallDir: *install, Staged: *staged, Data: *data, CurrentVersion: *current, Keys: keys,
		ConfirmTimeout: 60 * time.Second,
		WaitParent:     func() error { return waitPID(*pid, 60*time.Second) },
		Start: func(exe string, args ...string) (selfupdate.Process, error) {
			cmd := exec.Command(exe, args...)
			cmd.Dir = filepath.Dir(exe)
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			return proc{cmd}, nil
		},
		OnSuccess: registerVersion,
	})
	if err != nil {
		log.Println("Actualización fallida:", err)
		os.Exit(1)
	}
}

type proc struct{ cmd *exec.Cmd }

func (p proc) Wait() error { return p.cmd.Wait() }
func (p proc) Kill() error { return p.cmd.Process.Kill() }

func waitPID(pid int, timeout time.Duration) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil // ya no existe
	}
	done := make(chan struct{})
	go func() { p.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("tiempo agotado")
	}
}
