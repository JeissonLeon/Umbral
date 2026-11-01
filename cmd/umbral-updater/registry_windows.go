package main

import (
	"log"

	"golang.org/x/sys/windows/registry"
)

// registerVersion mantiene al día "Agregar o quitar programas".
func registerVersion(version string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\Umbral`, registry.SET_VALUE)
	if err != nil {
		log.Println("No se pudo actualizar la versión en Windows:", err)
		return
	}
	defer k.Close()
	k.SetStringValue("DisplayVersion", version)
}
