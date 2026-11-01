package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"umbral/internal/pack"
	"umbral/internal/selfupdate"
)

// Estados de la actividad del launcher.
const (
	idle      = ""
	preparing = "preparando"
	playing   = "jugando"
	loggingIn = "sesion"
	checking  = "comprobando"
	updating  = "actualizando-launcher"
)

// recheckEvery es cada cuánto se buscan novedades con el launcher abierto.
const recheckEvery = 30 * time.Minute

type App struct {
	paths     Paths
	token     string
	keys      []ed25519.PublicKey
	installer *pack.Installer

	mu          sync.Mutex
	cfg         Config
	manifest    *signedManifest
	plan        *pack.Plan
	acc         *Account
	busy        string
	lastError   string
	notice      string
	offline     bool
	server      *ServerStatus
	cancelLogin context.CancelFunc
	cancelOp    context.CancelFunc
	pingWake    chan struct{}
	closeWindow func()
}

func newApp(paths Paths) (*App, error) {
	cfg, err := loadConfig(paths)
	if err != nil {
		return nil, err
	}
	keys, err := pack.TrustedKeys()
	if err != nil {
		return nil, err
	}
	// Las versiones anteriores guardaban aquí la sesión de Microsoft.
	os.Remove(filepath.Join(paths.Root, "auth.json"))

	a := &App{
		paths:    paths,
		token:    randomString(24),
		keys:     keys,
		cfg:      cfg,
		manifest: cachedManifest(paths, keys),
		pingWake: make(chan struct{}, 1),
	}
	a.installer = &pack.Installer{
		Root: paths.Instance,
		Dir:  filepath.Join(paths.Root, "estado"),
		Enabled: func(p string) bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			return slices.Contains(a.cfg.Extras, p)
		},
		OnProgress: func(p pack.Progress) { hub.send("descarga", p) },
	}
	if err := a.installer.Recover(); err != nil {
		log.Println("No se pudo recuperar la actualización interrumpida:", err)
		a.lastError = "No se pudo recuperar una actualización interrumpida: " + err.Error()
	}
	go func() {
		for {
			if a.begin(checking) == nil {
				a.broadcast()
				a.refresh()
				a.finish()
			}
			time.Sleep(recheckEvery)
		}
	}()
	go a.pingLoop()
	return a, nil
}

// refresh consulta el manifiesto publicado y calcula qué habría que cambiar.
// Sin conexión sigue con la última copia verificada.
func (a *App) refresh() {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	var err error
	m := a.currentManifest()
	if src := cfg.manifestURL(); src != "" {
		var fresh *signedManifest
		if fresh, err = fetchManifest(a.paths, src, a.keys); err == nil {
			m = fresh
		} else {
			log.Println("No se pudo consultar el manifiesto:", err)
		}
	}
	var plan *pack.Plan
	var planErr error
	if m != nil {
		plan, planErr = a.installer.Plan(m.Manifest)
	}
	a.mu.Lock()
	a.manifest, a.plan = m, plan
	a.offline = err != nil
	if errors.Is(err, pack.ErrBadSignature) {
		a.lastError = "Se rechazó la actualización publicada: su firma no es válida."
	} else if err != nil && m == nil {
		a.lastError = "No se pudo descargar la información del modpack: " + err.Error()
	} else if planErr != nil {
		a.lastError = planErr.Error()
	}
	a.mu.Unlock()
}

func (a *App) currentManifest() *signedManifest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.manifest
}

type modView struct {
	Name    string `json:"nombre"`
	Desc    string `json:"descripcion"`
	Path    string `json:"archivo"`
	Enabled bool   `json:"activo"`
}

type updateView struct {
	Pending   bool     `json:"pendiente"`
	Installed string   `json:"instalada"`
	Available string   `json:"disponible"`
	Add       []string `json:"anadir"`
	Update    []string `json:"actualizar"`
	Delete    []string `json:"eliminar"`
	Bytes     int64    `json:"bytes"`
}

type stateView struct {
	Version    string        `json:"version_launcher"`
	Modpack    any           `json:"modpack"`
	Account    any           `json:"cuenta"`
	Config     any           `json:"ajustes"`
	Mods       []modView     `json:"mods"`
	Extras     []modView     `json:"extras"`
	Busy       string        `json:"actividad"`
	Error      string        `json:"error"`
	Notice     string        `json:"aviso"`
	Installed  bool          `json:"instalado"`
	Configured bool          `json:"configurado"`
	Offline    bool          `json:"sin_conexion"`
	CanLogin   bool          `json:"login_configurado"`
	Update     updateView    `json:"actualizacion"`
	Launcher   string        `json:"launcher_nuevo"`
	Server     *ServerStatus `json:"servidor"`
	DataDir    string        `json:"carpeta"`
}

func (a *App) snapshot() stateView {
	st, _ := a.installer.LoadState()
	a.mu.Lock()
	defer a.mu.Unlock()
	s := stateView{
		Version: launcherVersion,
		Config: map[string]any{
			"servidor": a.cfg.Server, "memoria_mb": a.cfg.MemoryMB, "modo": a.cfg.mode(),
			"carpeta_minecraft": a.cfg.officialDir(), "java_ruta": a.cfg.JavaPath, "java_argumentos": a.cfg.JavaArgs,
		},
		Busy:       a.busy,
		Error:      a.lastError,
		Notice:     a.notice,
		Configured: a.cfg.manifestURL() != "" && len(a.keys) > 0,
		Offline:    a.offline,
		CanLogin:   a.cfg.clientID() != "",
		Server:     a.server,
		DataDir:    a.paths.Root,
	}
	if a.acc != nil {
		s.Account = map[string]string{"nombre": a.acc.Name, "uuid": a.acc.UUID}
	}
	s.Mods, s.Extras = []modView{}, []modView{}
	if st != nil && st.Sequence > 0 {
		s.Update.Installed = st.Version
	}
	m := a.manifest
	if m == nil {
		s.Modpack = map[string]any{"nombre": "SylvanΩr 3"}
		return s
	}
	s.Modpack = map[string]any{
		"nombre": m.Modpack.Name, "version": m.Modpack.Version,
		"minecraft": m.Modpack.Minecraft, "forge": m.Modpack.Forge, "java": m.Modpack.Java,
		"umbral": m.UmbralMod.Version, "novedades": m.News,
	}
	for _, f := range m.Files {
		if !strings.HasPrefix(f.Path, "mods/") {
			continue
		}
		name := f.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(f.Path), ".jar")
		}
		if f.Required {
			s.Mods = append(s.Mods, modView{name, f.Description, f.Path, true})
		} else {
			s.Extras = append(s.Extras, modView{name, f.Description, f.Path, slices.Contains(a.cfg.Extras, f.Path)})
		}
	}
	id := m.Modpack.Minecraft + "-forge-" + m.Modpack.Forge
	_, err := os.Stat(filepath.Join(a.paths.Versions, id, id+".json"))
	s.Installed = err == nil && st != nil && st.Sequence > 0
	if p := a.plan; p != nil {
		s.Update.Available = m.Modpack.Version
		s.Update.Bytes = p.Bytes
		s.Update.Delete = p.Delete
		for _, f := range p.Add {
			s.Update.Add = append(s.Update.Add, f.Path)
		}
		for _, f := range p.Update {
			s.Update.Update = append(s.Update.Update, f.Path)
		}
		s.Update.Pending = !p.Empty()
	}
	if selfupdate.Available(m.Manifest, launcherVersion, a.paths.Root) {
		s.Launcher = m.Launcher.Version
	}
	return s
}

func (a *App) broadcast() { hub.send("estado", a.snapshot()) }

func (a *App) fail(err error) {
	log.Println("Error:", err)
	a.mu.Lock()
	a.busy = idle
	a.cancelOp = nil
	if errors.Is(err, context.Canceled) {
		a.lastError = ""
		a.notice = "Operación cancelada. Puedes continuar cuando quieras: se retoma donde iba."
	} else {
		a.lastError = err.Error()
	}
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
	a.notice = ""
	return nil
}

func (a *App) finish() {
	a.mu.Lock()
	a.busy = idle
	a.cancelOp = nil
	a.mu.Unlock()
	a.broadcast()
}

// checkNow busca novedades y revisa los archivos instalados.
func (a *App) checkNow() error {
	if err := a.begin(checking); err != nil {
		return err
	}
	a.broadcast()
	go func() {
		a.refresh()
		a.finish()
	}()
	return nil
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
		acc, err := browserLogin(ctx, clientID)
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
	go a.broadcast()
	return nil
}

// settingsInput son los ajustes que puede cambiar el jugador.
type settingsInput struct {
	Server       string `json:"servidor"`
	MemoryMB     int    `json:"memoria_mb"`
	Mode         string `json:"modo"`
	MinecraftDir string `json:"carpeta_minecraft"`
	JavaPath     string `json:"java_ruta"`
	JavaArgs     string `json:"java_argumentos"`
}

func (a *App) updateSettings(in settingsInput) error {
	in.Server = strings.TrimSpace(in.Server)
	if in.Server != "" {
		if _, _, err := splitServer(in.Server); err != nil {
			return fmt.Errorf("dirección del servidor: %w", err)
		}
	}
	in.JavaPath = strings.TrimSpace(in.JavaPath)
	if in.JavaPath != "" {
		want := 17
		if m := a.currentManifest(); m != nil {
			want = m.Modpack.Java
		}
		if err := checkJava(in.JavaPath, want); err != nil {
			return err
		}
	}
	in.MinecraftDir = strings.TrimSpace(in.MinecraftDir)
	if in.MinecraftDir != "" && !filepath.IsAbs(in.MinecraftDir) {
		return errors.New("la carpeta de Minecraft debe ser una ruta completa")
	}
	switch in.Mode {
	case modeOfficial, modeExternal:
	case modeMicrosoft:
		if a.cfg.clientID() == "" {
			return errors.New("este launcher se compiló sin inicio directo de Microsoft")
		}
	default:
		return errors.New("modo de inicio desconocido")
	}
	a.mu.Lock()
	a.cfg.Server = in.Server
	a.cfg.MemoryMB = clampMemory(in.MemoryMB)
	a.cfg.LaunchMode = in.Mode
	a.cfg.MinecraftDir = in.MinecraftDir
	a.cfg.JavaPath = in.JavaPath
	a.cfg.JavaArgs = strings.TrimSpace(in.JavaArgs)
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

// moveData elige otra carpeta para el juego. No mueve ni borra la anterior:
// la instalación se hace de nuevo allí y la vieja queda a decisión del jugador.
func (a *App) moveData(dir string) error {
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		return errors.New("escribe una ruta completa")
	}
	a.mu.Lock()
	busy := a.busy
	cfg := a.cfg
	a.mu.Unlock()
	if busy != idle {
		return errBusy
	}
	if err := checkWritable(dir); err != nil {
		return err
	}
	if err := saveConfig(Paths{Root: dir}, cfg); err != nil {
		return err
	}
	if err := setDataLocation(dir); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := exec.Command(exe).Start(); err != nil {
		return err
	}
	a.closeApp()
	return nil
}

func (a *App) closeApp() {
	a.mu.Lock()
	closeWindow := a.closeWindow
	a.mu.Unlock()
	if closeWindow != nil {
		closeWindow()
		return
	}
	os.Exit(0)
}

// setExtra activa o desactiva un archivo opcional. Se aplica al instalar.
func (a *App) setExtra(file string, enabled bool) error {
	m := a.currentManifest()
	a.mu.Lock()
	allowed := m != nil && slices.ContainsFunc(m.Files, func(f pack.File) bool { return f.Path == file && !f.Required })
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
	if m != nil {
		if plan, err := a.installer.Plan(m.Manifest); err == nil {
			a.mu.Lock()
			a.plan = plan
			a.mu.Unlock()
		}
	}
	a.broadcast()
	return nil
}

// startOp arranca una operación larga cancelable.
func (a *App) startOp(state string, run func(ctx context.Context) error) error {
	if err := a.begin(state); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancelOp = cancel
	a.mu.Unlock()
	a.broadcast()
	go func() {
		defer cancel()
		if err := run(ctx); err != nil {
			a.fail(err)
			return
		}
		a.finish()
	}()
	return nil
}

func (a *App) cancelOperation() {
	a.mu.Lock()
	cancel := a.cancelOp
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// install deja la instancia igual que el manifiesto publicado, sin abrir el juego.
func (a *App) install() error {
	return a.startOp(preparing, func(ctx context.Context) error {
		_, err := a.prepare(ctx)
		return err
	})
}

func (a *App) play() error {
	return a.startOp(preparing, a.prepareAndLaunch)
}

// prepared es lo que deja listo prepare.
type prepared struct {
	m       *signedManifest
	java    string
	vanilla *VersionJSON
	forgeID string
}

func (a *App) prepare(ctx context.Context) (*prepared, error) {
	hub.stage("Buscando actualizaciones")
	a.refresh()
	a.mu.Lock()
	m, cfg, plan := a.manifest, a.cfg, a.plan
	a.mu.Unlock()
	if m == nil {
		if cfg.manifestURL() == "" || len(a.keys) == 0 {
			return nil, errors.New("este launcher se compiló sin dirección de publicación o sin clave pública; mira docs/ADMINISTRAR_GITHUB.md")
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		return nil, errors.New(a.lastError)
	}
	if plan == nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		return nil, errors.New(a.lastError)
	}
	vanilla, err := ensureVanilla(a.paths, m.Modpack.Minecraft)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	java := cfg.JavaPath
	if java != "" {
		if err := checkJava(java, m.Modpack.Java); err != nil {
			return nil, err
		}
	} else if java, err = ensureJava(a.paths, vanilla.JavaVersion.Component); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	forgeID, err := ensureForge(a.paths, java, m.Modpack.Minecraft, m.Modpack.Forge, m.Modpack.ForgeInstallerSHA256)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hub.stage("Sincronizando archivos del modpack")
	if err := a.installer.Apply(ctx, m.Manifest, plan); err != nil {
		return nil, err
	}
	if err := quarantineForeignMods(a.paths, m.Manifest, a.installer.Enabled); err != nil {
		return nil, err
	}
	// Comprobación final contra el disco: si algo no cuadra, no consta como listo.
	after, err := a.installer.Plan(m.Manifest)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.plan = after
	a.mu.Unlock()
	return &prepared{m, java, vanilla, forgeID}, nil
}

func (a *App) prepareAndLaunch(ctx context.Context) error {
	a.mu.Lock()
	cfg, acc := a.cfg, a.acc
	a.mu.Unlock()

	if cfg.mode() == modeMicrosoft {
		hub.stage("Comprobando tu cuenta")
		fresh, err := ensureSession(cfg.clientID(), acc)
		a.mu.Lock()
		a.acc = fresh
		a.mu.Unlock()
		if err != nil {
			return err
		}
		acc = fresh
	}
	p, err := a.prepare(ctx)
	if err != nil {
		return err
	}
	req := launchRequest{a.paths, cfg, p.java, p.vanilla, p.forgeID, p.m.Modpack.Name}

	hub.stage("Abriendo Minecraft")
	var res *launchResult
	switch cfg.mode() {
	case modeExternal:
		res, err = launchExternal(ctx, req)
	case modeMicrosoft:
		res, err = launchDirect(ctx, req, acc)
	default:
		res, err = launchOfficial(ctx, req)
	}
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.notice = res.notice
	if res.wait != nil {
		a.busy = playing
	}
	a.mu.Unlock()
	a.broadcast()
	if res.wait == nil {
		return nil
	}
	started := time.Now()
	err = res.wait()
	log.Printf("El juego se cerró tras %s", time.Since(started).Round(time.Second))
	if err != nil {
		return fmt.Errorf("Minecraft se cerró con un error. Revisa %s y la carpeta crash-reports", res.log)
	}
	return nil
}

func launchDirect(_ context.Context, r launchRequest, acc *Account) (*launchResult, error) {
	wait, logPath, err := startGame(r.paths, r.cfg, acc, r.java, r.vanilla, r.forgeID)
	if err != nil {
		return nil, err
	}
	return &launchResult{wait: wait, log: logPath}, nil
}

// updateLauncher descarga y verifica la versión nueva, y deja que el
// actualizador haga el cambio cuando este proceso se cierre.
func (a *App) updateLauncher() error {
	m := a.currentManifest()
	if m == nil || !selfupdate.Available(m.Manifest, launcherVersion, a.paths.Root) {
		return errors.New("no hay ninguna versión nueva del launcher")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	install := filepath.Dir(exe)
	updater := filepath.Join(install, selfupdate.UpdaterExe)
	if _, err := os.Stat(updater); err != nil {
		return fmt.Errorf("falta %s; reinstala Umbral con UmbralSetup.exe", selfupdate.UpdaterExe)
	}
	return a.startOp(updating, func(ctx context.Context) error {
		hub.stage("Descargando Umbral Launcher " + m.Launcher.Version)
		var total, done int64
		for _, f := range m.Launcher.Files {
			total += f.Size
		}
		staged, err := selfupdate.Stage(ctx, m.Manifest, m.raw, m.sig, a.paths.Root, func(n int64) {
			done += n
			hub.send("descarga", pack.Progress{File: selfupdate.LauncherExe, BytesDone: done, BytesTotal: total, FilesTotal: 1})
		})
		if err != nil {
			return err
		}
		if err := selfupdate.SwapUpdater(staged, install); err != nil {
			return fmt.Errorf("no se pudo preparar el actualizador: %w", err)
		}
		cmd := exec.Command(updater, "--pid", fmt.Sprint(os.Getpid()), "--install", install, "--staged", staged, "--data", a.paths.Root, "--current", launcherVersion)
		hideConsole(cmd)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("no se pudo abrir el actualizador: %w", err)
		}
		log.Println("Actualizador en marcha; cerrando el launcher")
		a.closeApp()
		return nil
	})
}

// diagnostics resume el estado para pedir ayuda; no incluye nada secreto.
func (a *App) diagnostics() map[string]any {
	st, _ := a.installer.LoadState()
	a.mu.Lock()
	cfg := a.cfg
	m := a.manifest
	a.mu.Unlock()
	keys := []string{}
	for _, k := range a.keys {
		keys = append(keys, pack.KeyID(k))
	}
	d := map[string]any{
		"launcher": launcherVersion, "carpeta": a.paths.Root, "instancia": a.paths.Instance,
		"modo": cfg.mode(), "manifiesto_url": cfg.manifestURL(), "claves": keys, "minecraft_oficial": cfg.officialDir(),
		"java": cfg.JavaPath, "memoria_mb": cfg.MemoryMB,
	}
	if st != nil {
		d["instalado_secuencia"], d["instalado_version"], d["archivos_registrados"] = st.Sequence, st.Version, len(st.Files)
	}
	if m != nil {
		d["manifiesto_secuencia"], d["manifiesto_version"], d["manifiesto_fecha"] = m.Sequence, m.Modpack.Version, m.PublishedAt
	}
	return d
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
	case "launcher":
		dir = a.paths.Root
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
