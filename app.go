package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Estados de la actividad del launcher.
const (
	idle      = ""
	preparing = "preparando"
	playing   = "jugando"
	loggingIn = "sesion"
)

type App struct {
	paths Paths
	token string

	mu          sync.Mutex
	cfg         Config
	manifest    Manifest
	acc         *Account
	busy        string
	lastError   string
	server      *ServerStatus
	cancelLogin context.CancelFunc
	pingWake    chan struct{}
}

func newApp(paths Paths) (*App, error) {
	cfg, err := loadConfig(paths)
	if err != nil {
		return nil, err
	}
	a := &App{
		paths:    paths,
		token:    randomString(24),
		cfg:      cfg,
		manifest: cachedManifest(paths),
		acc:      loadAccount(paths),
		pingWake: make(chan struct{}, 1),
	}
	go func() {
		// El manifiesto remoto puede tardar; la interfaz arranca con el interno.
		if src := cfg.manifestURL(); src != "" {
			m := loadManifest(paths, src)
			a.mu.Lock()
			a.manifest = m
			a.mu.Unlock()
			a.broadcast()
		}
	}()
	go a.pingLoop()
	return a, nil
}

type modView struct {
	Name    string `json:"nombre"`
	Desc    string `json:"descripcion"`
	Side    string `json:"lado"`
	File    string `json:"archivo"`
	Enabled bool   `json:"activo"`
}

type stateView struct {
	Version   string        `json:"version_launcher"`
	Modpack   any           `json:"modpack"`
	Account   any           `json:"cuenta"`
	Config    any           `json:"ajustes"`
	Mods      []modView     `json:"mods"`
	Extras    []modView     `json:"extras"`
	Busy      string        `json:"actividad"`
	Error     string        `json:"error"`
	Installed bool          `json:"instalado"`
	CanLogin  bool          `json:"login_configurado"`
	Server    *ServerStatus `json:"servidor"`
	DataDir   string        `json:"carpeta"`
}

func (a *App) snapshot() stateView {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.manifest
	s := stateView{
		Version: launcherVersion,
		Modpack: map[string]any{
			"nombre": m.Name, "version": m.ModpackVersion,
			"minecraft": m.Minecraft, "forge": m.Forge, "novedades": m.News,
		},
		Config:   map[string]any{"servidor": a.cfg.Server, "memoria_mb": a.cfg.MemoryMB},
		Busy:     a.busy,
		Error:    a.lastError,
		CanLogin: a.cfg.clientID() != "",
		Server:   a.server,
		DataDir:  a.paths.Root,
	}
	if a.acc != nil {
		s.Account = map[string]string{"nombre": a.acc.Name, "uuid": a.acc.UUID}
	}
	for _, mod := range m.Mods {
		s.Mods = append(s.Mods, modView{mod.Name, mod.Desc, mod.Side, mod.File, true})
	}
	for _, mod := range m.Extras {
		s.Extras = append(s.Extras, modView{mod.Name, mod.Desc, mod.Side, mod.File, slices.Contains(a.cfg.Extras, mod.File)})
	}
	id := m.Minecraft + "-forge-" + m.Forge
	_, err := os.Stat(filepath.Join(a.paths.Versions, id, id+".json"))
	s.Installed = err == nil
	return s
}

func (a *App) broadcast() { hub.send("estado", a.snapshot()) }

func (a *App) fail(err error) {
	log.Println("Error:", err)
	a.mu.Lock()
	a.busy = idle
	a.lastError = err.Error()
	a.mu.Unlock()
	a.broadcast()
}

var errBusy = errors.New("el launcher está ocupado; espera a que termine")

// begin marca el launcher como ocupado si estaba libre.
func (a *App) begin(state string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy != idle {
		return errBusy
	}
	a.busy = state
	a.lastError = ""
	return nil
}

func (a *App) finish() {
	a.mu.Lock()
	a.busy = idle
	a.mu.Unlock()
	a.broadcast()
}

func (a *App) login() error {
	if err := a.begin(loggingIn); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancelLogin = cancel
	clientID := a.cfg.clientID()
	a.mu.Unlock()
	a.broadcast()
	go func() {
		defer cancel()
		acc, err := browserLogin(ctx, clientID, a.paths)
		if errors.Is(err, errLoginCanceled) {
			a.finish()
			return
		}
		if err != nil {
			a.fail(err)
			return
		}
		log.Println("Sesión iniciada como", acc.Name)
		a.mu.Lock()
		a.acc = acc
		a.mu.Unlock()
		a.finish()
	}()
	return nil
}

func (a *App) cancelLoginAttempt() {
	a.mu.Lock()
	cancel := a.cancelLogin
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) logout() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy != idle {
		return errBusy
	}
	a.acc = nil
	deleteAccount(a.paths)
	go a.broadcast()
	return nil
}

func (a *App) updateSettings(server string, memoryMB int) error {
	server = strings.TrimSpace(server)
	if server != "" {
		if _, _, err := splitServer(server); err != nil {
			return fmt.Errorf("dirección del servidor: %w", err)
		}
	}
	a.mu.Lock()
	a.cfg.Server = server
	a.cfg.MemoryMB = clampMemory(memoryMB)
	a.server = nil
	cfg := a.cfg
	a.mu.Unlock()
	if err := saveConfig(a.paths, cfg); err != nil {
		return err
	}
	select {
	case a.pingWake <- struct{}{}:
	default:
	}
	a.broadcast()
	return nil
}

// setExtra activa o desactiva un mod extra. Se aplica la próxima vez que
// se pulse Jugar.
func (a *App) setExtra(file string, enabled bool) error {
	a.mu.Lock()
	allowed := slices.ContainsFunc(a.manifest.Extras, func(m Mod) bool { return m.File == file })
	if !allowed {
		a.mu.Unlock()
		return errors.New("ese mod no está en la lista permitida")
	}
	extras := slices.DeleteFunc(slices.Clone(a.cfg.Extras), func(f string) bool { return f == file })
	if enabled {
		extras = append(extras, file)
	}
	a.cfg.Extras = extras
	cfg := a.cfg
	a.mu.Unlock()
	if err := saveConfig(a.paths, cfg); err != nil {
		return err
	}
	a.broadcast()
	return nil
}

func (a *App) play() error {
	if err := a.begin(preparing); err != nil {
		return err
	}
	a.broadcast()
	go func() {
		if err := a.prepareAndLaunch(); err != nil {
			a.fail(err)
		}
	}()
	return nil
}

func (a *App) prepareAndLaunch() error {
	a.mu.Lock()
	cfg, acc := a.cfg, a.acc
	a.mu.Unlock()

	hub.stage("Comprobando tu cuenta")
	acc, err := ensureSession(cfg.clientID(), a.paths, acc)
	a.mu.Lock()
	a.acc = acc
	a.mu.Unlock()
	if err != nil {
		return err
	}

	hub.stage("Buscando actualizaciones del modpack")
	m := loadManifest(a.paths, cfg.manifestURL())
	a.mu.Lock()
	a.manifest = m
	a.mu.Unlock()

	vanilla, err := ensureVanilla(a.paths, m.Minecraft)
	if err != nil {
		return err
	}
	java, err := ensureJava(a.paths, vanilla.JavaVersion.Component)
	if err != nil {
		return err
	}
	forgeID, err := ensureForge(a.paths, java, m.Minecraft, m.Forge)
	if err != nil {
		return err
	}
	var extras []Mod
	for _, e := range m.Extras {
		if slices.Contains(cfg.Extras, e.File) {
			extras = append(extras, e)
		}
	}
	if err := ensureMods(a.paths, m.Mods, extras); err != nil {
		return err
	}

	hub.stage("Abriendo Minecraft")
	wait, logPath, err := startGame(a.paths, cfg, acc, java, vanilla, forgeID)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.busy = playing
	a.mu.Unlock()
	a.broadcast()

	started := time.Now()
	err = wait()
	log.Printf("El juego se cerró tras %s", time.Since(started).Round(time.Second))
	if err != nil {
		return fmt.Errorf("Minecraft se cerró con un error. Revisa %s y la carpeta crash-reports", logPath)
	}
	a.finish()
	return nil
}

func (a *App) openFolder(which string) error {
	var dir string
	switch which {
	case "juego":
		dir = a.paths.Instance
	case "mods":
		dir = filepath.Join(a.paths.Instance, "mods")
	case "registros":
		dir = filepath.Join(a.paths.Instance, "logs")
	case "capturas":
		dir = filepath.Join(a.paths.Instance, "screenshots")
	default:
		return errors.New("carpeta desconocida")
	}
	os.MkdirAll(dir, 0o755)
	openPath(dir)
	return nil
}

// pingLoop consulta el estado del servidor cada 30 segundos.
func (a *App) pingLoop() {
	for {
		a.mu.Lock()
		addr := a.cfg.Server
		a.mu.Unlock()
		if addr != "" {
			st := pingServer(addr)
			a.mu.Lock()
			if a.cfg.Server == addr {
				a.server = &st
			}
			a.mu.Unlock()
			hub.send("servidor", st)
		}
		select {
		case <-a.pingWake:
		case <-time.After(30 * time.Second):
		}
	}
}
