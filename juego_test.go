//go:build juego

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Abre Minecraft de verdad con una instancia ya instalada, sin cuenta real (solo
// para esta prueba: el launcher no tiene ningún modo sin cuenta).
//
//	UMBRAL_HOME=<carpeta instalada> go test -tags juego -run TestArranqueReal -timeout 10m .
func TestArranqueReal(t *testing.T) {
	paths, err := newPaths()
	if err != nil {
		t.Fatal(err)
	}
	vanilla, err := readVersion(paths, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	java := javaBinary(filepath.Join(paths.Runtime, vanilla.JavaVersion.Component))
	acc := &Account{Name: "PruebaUmbral", UUID: "00000000000000000000000000000000", AccessToken: "0"}
	args, err := buildLaunchArgs(paths, Config{MemoryMB: 4096}, acc, vanilla, "1.20.1-forge-47.4.10")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) == 0 {
		t.Fatal("sin argumentos")
	}
	wait, logPath, err := startGame(paths, Config{MemoryMB: 4096}, acc, java, vanilla, "1.20.1-forge-47.4.10")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- wait() }()
	latest := filepath.Join(paths.Instance, "logs", "latest.log")
	deadline := time.After(8 * time.Minute)
	for {
		select {
		case err := <-done:
			data, _ := os.ReadFile(logPath)
			t.Fatalf("el juego se cerró antes de tiempo (%v):\n%s", err, tail(string(data)))
		case <-deadline:
			data, _ := os.ReadFile(latest)
			t.Fatalf("no llegó al menú:\n%s", tail(string(data)))
		case <-time.After(3 * time.Second):
			data, _ := os.ReadFile(latest)
			text := string(data)
			if strings.Contains(text, "Created: 1024x") || strings.Contains(text, "Stitching") || strings.Contains(text, "Sound engine started") {
				if !strings.Contains(text, "umbral_sombras") {
					t.Error("el registro no menciona el mod Umbral")
				}
				t.Log(tail(text))
				return // la prueba la cierra el sistema operativo con taskkill
			}
		}
	}
}

func tail(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 25 {
		lines = lines[len(lines)-25:]
	}
	return strings.Join(lines, "\n")
}
