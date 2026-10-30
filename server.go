package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"
)

// serve arranca la API y la interfaz en 127.0.0.1 con un puerto libre.
// Solo responde a peticiones con el Host exacto (evita DNS rebinding) y,
// en la API, con el token de esta sesión (evita que otras webs la usen).
func (a *App) serve() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	host := ln.Addr().String()

	mux := http.NewServeMux()
	ui, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServerFS(ui))
	mux.HandleFunc("GET /api/estado", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.snapshot()) })
	mux.HandleFunc("GET /api/eventos", a.events)
	mux.HandleFunc("POST /api/jugar", action(func(*http.Request) error { return a.play() }))
	mux.HandleFunc("POST /api/sesion", action(func(*http.Request) error { return a.login() }))
	mux.HandleFunc("POST /api/sesion/cancelar", action(func(*http.Request) error { a.cancelLoginAttempt(); return nil }))
	mux.HandleFunc("POST /api/sesion/cerrar", action(func(*http.Request) error { return a.logout() }))
	mux.HandleFunc("POST /api/ajustes", action(func(r *http.Request) error {
		var body struct {
			Server   string `json:"servidor"`
			MemoryMB int    `json:"memoria_mb"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return err
		}
		return a.updateSettings(body.Server, body.MemoryMB)
	}))
	mux.HandleFunc("POST /api/extra", action(func(r *http.Request) error {
		var body struct {
			File    string `json:"archivo"`
			Enabled bool   `json:"activo"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return err
		}
		return a.setExtra(body.File, body.Enabled)
	}))
	mux.HandleFunc("POST /api/abrir", action(func(r *http.Request) error {
		var body struct {
			Folder string `json:"carpeta"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return err
		}
		return a.openFolder(body.Folder)
	}))

	guard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "Host no permitido", http.StatusForbidden)
			return
		}
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			tok := r.Header.Get("X-Umbral-Token")
			if tok == "" {
				tok = r.URL.Query().Get("t") // EventSource no admite cabeceras
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(a.token)) != 1 {
				http.Error(w, "No autorizado", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Handler: guard, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	return fmt.Sprintf("http://%s/?t=%s", host, a.token), nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func action(f func(*http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(r); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

func (a *App) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "sin streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	ch := hub.subscribe()
	defer hub.unsubscribe(ch)

	first, _ := json.Marshal(map[string]any{"tipo": "estado", "datos": a.snapshot()})
	fmt.Fprintf(w, "data: %s\n\n", first)
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
