// umbral-publisher prepara y publica una versión del modpack: calcula hashes y
// tamaños, genera y firma el manifiesto, y sube todo a GitHub Releases con gh.
//
//	umbral-publisher keygen  --out <carpeta>
//	umbral-publisher release --version 1.0.0 [opciones]
//	umbral-publisher verify  --dir <salida> --public <clave>
//	umbral-publisher publish --dir <salida>
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"umbral/internal/pack"
	"umbral/internal/selfupdate"
)

func main() {
	if len(os.Args) < 2 {
		fatal(usage)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "release":
		err = release(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	case "publish":
		err = publish(os.Args[2:])
	default:
		fatal(usage)
	}
	if err != nil {
		fatal("Error: " + err.Error())
	}
}

const usage = "uso: umbral-publisher keygen|release|verify|publish [opciones]; mira docs/PUBLICAR_MODPACK.md"

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "", "carpeta para la clave privada, fuera del repositorio")
	fs.Parse(args)
	if *out == "" {
		return fmt.Errorf("falta --out")
	}
	privPath := filepath.Join(*out, "umbral-firma.key")
	if _, err := os.Stat(privPath); err == nil {
		return fmt.Errorf("%s ya existe; no se sobrescribe una clave", privPath)
	}
	pub, priv, err := pack.GenerateKey()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(privPath, []byte(priv+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("Clave privada: %s (guárdala fuera del repositorio y haz una copia)\n", privPath)
	fmt.Printf("Clave pública, añádela a internal/pack/trusted_keys.txt:\n%s\n", pub)
	return nil
}

// Config es publicacion.json.
type Config struct {
	Modpack struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Minecraft string `json:"minecraft"`
		Forge     string `json:"forge"`
		Java      int    `json:"java"`
	} `json:"modpack"`
	GitHub struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
	} `json:"github"`
	Content   string     `json:"contenido"`
	External  []External `json:"externos"`
	UmbralMod string     `json:"umbralMod"`
	Seed      []string   `json:"seed"`
	Optional  []string   `json:"opcionales"`
	News      []string   `json:"novedades"`
}

// External es un archivo que se descarga de su sitio original (Modrinth, por
// ejemplo) en lugar de volver a alojarlo, respetando la licencia de cada mod.
type External struct {
	Path        string `json:"path"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Optional    bool   `json:"optional"`
}

// Output es lo que sale de release.
type Output struct {
	Manifest *pack.Manifest
	Upload   []string
}

func release(args []string) error {
	fs := flag.NewFlagSet("release", flag.ExitOnError)
	cfgPath := fs.String("config", "publicacion/publicacion.json", "configuración")
	version := fs.String("version", "", "versión del modpack")
	launcherVersion := fs.String("launcher-version", "", "versión del launcher a publicar")
	launcherDir := fs.String("launcher-dir", "", "carpeta con UmbralLauncher.exe y umbral-updater.exe")
	setup := fs.String("setup", "", "instalador UmbralSetup.exe a incluir en la versión")
	previous := fs.String("previous", "", "manifest.json anterior: ruta o URL; 'latest' lo baja de GitHub; 'none' para la primera")
	keyFile := fs.String("key-file", "", "clave privada; si no, la variable UMBRAL_SIGNING_KEY")
	out := fs.String("out", "dist/release", "carpeta de salida")
	baseURL := fs.String("base-url", "", "solo para ensayos: sirve los archivos desde otra dirección")
	fs.Parse(args)

	if *version == "" {
		return fmt.Errorf("falta --version")
	}
	keyText := os.Getenv("UMBRAL_SIGNING_KEY")
	if *keyFile != "" {
		data, err := os.ReadFile(*keyFile)
		if err != nil {
			return err
		}
		keyText = string(data)
	}
	if keyText == "" {
		return fmt.Errorf("falta la clave: usa --key-file o UMBRAL_SIGNING_KEY")
	}
	key, err := pack.ParsePrivateKey(keyText)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*cfgPath)
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("%s: %w", *cfgPath, err)
	}
	if cfg.Content != "" && !filepath.IsAbs(cfg.Content) {
		cfg.Content = filepath.Join(filepath.Dir(*cfgPath), cfg.Content)
	}
	prev, err := loadPrevious(*previous, cfg)
	if err != nil {
		return err
	}
	result, err := build(context.Background(), cfg, prev, buildOptions{
		Version: *version, LauncherVersion: *launcherVersion, LauncherDir: *launcherDir, Setup: *setup, Out: *out,
		BaseURL: baseURLFor(*baseURL, cfg),
	})
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(result.Manifest, "", "  ")
	if err != nil {
		return err
	}
	// Se relee y valida lo mismo que comprobará el launcher.
	if _, err := pack.Parse(data); err != nil {
		return err
	}
	sig, err := pack.Sign(data, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "manifest.json"), data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "manifest.json.sig"), sig, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s %s (secuencia %d): %d archivos, %d retirados, %d por subir. Salida en %s\n",
		cfg.Modpack.Name, *version, result.Manifest.Sequence, len(result.Manifest.Files), len(result.Manifest.Remove), len(result.Upload), *out)
	return nil
}

func baseURLFor(override string, cfg Config) string {
	if override != "" {
		return strings.TrimSuffix(override, "/")
	}
	return fmt.Sprintf("https://github.com/%s/%s/releases/download", cfg.GitHub.Owner, cfg.GitHub.Repo)
}

func loadPrevious(src string, cfg Config) (*pack.Manifest, error) {
	switch src {
	case "":
		return nil, fmt.Errorf("falta --previous: indica el manifest.json anterior, 'latest' o 'none' si es la primera versión")
	case "none":
		return nil, nil
	case "latest":
		src = fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/manifest.json", cfg.GitHub.Owner, cfg.GitHub.Repo)
	}
	var data []byte
	if strings.HasPrefix(src, "http") {
		resp, err := pack.Client.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s respondió %s", src, resp.Status)
		}
		if data, err = io.ReadAll(resp.Body); err != nil {
			return nil, err
		}
	} else {
		var err error
		if data, err = os.ReadFile(src); err != nil {
			return nil, err
		}
	}
	return pack.Parse(data)
}

type buildOptions struct {
	Version, LauncherVersion, LauncherDir, Setup, Out, BaseURL string
}

// build calcula el manifiesto y deja en Out/assets lo que hay que subir. No
// firma, para poder probarlo sin claves.
func build(ctx context.Context, cfg Config, prev *pack.Manifest, o buildOptions) (*Output, error) {
	assets := filepath.Join(o.Out, "assets")
	if err := os.RemoveAll(o.Out); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(assets, 0o755); err != nil {
		return nil, err
	}
	seq := int64(1)
	var prevFiles = map[string]pack.File{}
	urlBySHA := map[string]string{}
	if prev != nil {
		seq = prev.Sequence + 1
		for _, f := range prev.Files {
			prevFiles[f.Path] = f
			urlBySHA[f.SHA256] = f.URL
		}
	}
	tag := fmt.Sprintf("%s-%s-%d", cfg.Modpack.ID, o.Version, seq)
	res := &Output{Manifest: &pack.Manifest{
		SchemaVersion: pack.SchemaVersion, Sequence: seq, PublishedAt: time.Now().UTC().Format(time.RFC3339),
		Modpack: pack.Modpack{ID: cfg.Modpack.ID, Name: cfg.Modpack.Name, Version: o.Version,
			Minecraft: cfg.Modpack.Minecraft, Forge: cfg.Modpack.Forge, Java: cfg.Modpack.Java},
		News: cfg.News, Files: []pack.File{}, Remove: []pack.Remove{},
	}}
	m := res.Manifest
	sha, err := forgeInstallerHash(ctx, cfg.Modpack.Minecraft, cfg.Modpack.Forge, o.Out)
	if err != nil {
		return nil, err
	}
	m.Modpack.ForgeInstallerSHA256 = sha

	upload := func(src, sum string) (string, error) {
		if u, ok := urlBySHA[sum]; ok {
			return u, nil // ya publicado: los launchers no lo notan y no se resube
		}
		name := sum[:12] + "-" + assetName(filepath.Base(src))
		dst := filepath.Join(assets, name)
		if _, err := os.Stat(dst); err != nil {
			if err := copyFile(src, dst); err != nil {
				return "", err
			}
			res.Upload = append(res.Upload, dst)
		}
		return o.BaseURL + "/" + tag + "/" + name, nil
	}
	optional := func(p string) bool { return contains(cfg.Optional, p) }
	policy := func(p string) string {
		if contains(cfg.Seed, p) {
			return pack.PolicySeed
		}
		return ""
	}

	seen := map[string]bool{}
	add := func(f pack.File) error {
		if err := pack.SafePath(f.Path); err != nil {
			return err
		}
		if seen[strings.ToLower(f.Path)] {
			return fmt.Errorf("%s aparece dos veces (contenido y externos)", f.Path)
		}
		seen[strings.ToLower(f.Path)] = true
		f.Version = f.SHA256[:12]
		f.Policy = policy(f.Path)
		m.Files = append(m.Files, f)
		return nil
	}
	if cfg.Content != "" {
		err := filepath.WalkDir(cfg.Content, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() == ".gitkeep" {
				return err
			}
			rel, err := filepath.Rel(cfg.Content, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			sum, err := pack.HashFile(p)
			if err != nil {
				return err
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			u, err := upload(p, sum)
			if err != nil {
				return err
			}
			return add(pack.File{Path: rel, URL: u, SHA256: sum, Size: fi.Size(), Required: !optional(rel)})
		})
		if err != nil {
			return nil, err
		}
	}
	for _, e := range cfg.External {
		f := pack.File{Path: e.Path, URL: e.URL, Required: !e.Optional, Name: e.Name, Description: e.Description}
		if old, ok := prevFiles[e.Path]; ok && old.URL == e.URL {
			f.SHA256, f.Size = old.SHA256, old.Size
		} else {
			f.SHA256, f.Size, err = hashRemote(ctx, e.URL, o.Out)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", e.Path, err)
			}
		}
		if err := add(f); err != nil {
			return nil, err
		}
	}
	if len(m.Files) == 0 {
		return nil, fmt.Errorf("no hay archivos: revisa %q y externos", cfg.Content)
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })

	// Lo que estaba y ya no está se retira. Se conservan las retiradas
	// anteriores para quien salte varias versiones.
	removed := map[string]bool{}
	if prev != nil {
		for _, f := range prev.Files {
			if !seen[strings.ToLower(f.Path)] {
				m.Remove = append(m.Remove, pack.Remove{Path: f.Path, SHA256: f.SHA256})
				removed[f.Path] = true
			}
		}
		for _, r := range prev.Remove {
			if !seen[strings.ToLower(r.Path)] && !removed[r.Path] {
				m.Remove = append(m.Remove, r)
				removed[r.Path] = true
			}
		}
	}

	if cfg.UmbralMod != "" {
		re, err := regexp.Compile(cfg.UmbralMod)
		if err != nil {
			return nil, fmt.Errorf("umbralMod: %w", err)
		}
		for _, f := range m.Files {
			if g := re.FindStringSubmatch(f.Path); g != nil {
				m.UmbralMod.Path = f.Path
				if len(g) > 1 {
					m.UmbralMod.Version = g[1]
				}
			}
		}
		if m.UmbralMod.Path == "" {
			return nil, fmt.Errorf("no se encontró el mod Umbral con el patrón %q", cfg.UmbralMod)
		}
	}

	switch {
	case o.LauncherDir != "":
		if o.LauncherVersion == "" {
			return nil, fmt.Errorf("--launcher-dir necesita --launcher-version")
		}
		m.Launcher.Version = o.LauncherVersion
		for _, name := range []string{selfupdate.LauncherExe, selfupdate.UpdaterExe} {
			p := filepath.Join(o.LauncherDir, name)
			fi, err := os.Stat(p)
			if err != nil {
				return nil, err
			}
			sum, err := pack.HashFile(p)
			if err != nil {
				return nil, err
			}
			u, err := upload(p, sum)
			if err != nil {
				return nil, err
			}
			m.Launcher.Files = append(m.Launcher.Files, pack.Asset{Name: name, URL: u, SHA256: sum, Size: fi.Size()})
		}
	case prev != nil:
		m.Launcher = prev.Launcher
	case o.LauncherVersion != "":
		m.Launcher.Version = o.LauncherVersion
	default:
		return nil, fmt.Errorf("la primera versión necesita --launcher-version")
	}
	if o.Setup != "" {
		dst := filepath.Join(assets, "UmbralSetup.exe")
		if err := copyFile(o.Setup, dst); err != nil {
			return nil, err
		}
		res.Upload = append(res.Upload, dst)
	}
	return res, m.Validate()
}

func forgeInstallerHash(ctx context.Context, mc, forge, out string) (string, error) {
	full := mc + "-" + forge
	u := "https://maven.minecraftforge.net/net/minecraftforge/forge/" + full + "/forge-" + full + "-installer.jar"
	sha, _, err := hashRemote(ctx, u, out)
	if err != nil {
		return "", fmt.Errorf("instalador de Forge: %w", err)
	}
	return sha, nil
}

// hashRemote se sustituye en las pruebas. Calcula hash y tamaño sin guardar
// el archivo.
var hashRemote = func(ctx context.Context, rawURL, _ string) (string, int64, error) {
	if err := pack.CheckURL(rawURL); err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := pack.Client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("%s respondió %s", rawURL, resp.Status)
	}
	tmp, err := os.CreateTemp("", "umbral-hash-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, resp.Body)
	tmp.Close()
	if err != nil {
		return "", 0, err
	}
	sum, err := pack.HashFile(tmp.Name())
	return sum, n, err
}

var unsafeAsset = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// assetName evita los caracteres que GitHub reescribe en los nombres.
func assetName(name string) string { return unsafeAsset.ReplaceAllString(name, "_") }

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	dir := fs.String("dir", "dist/release", "salida de release")
	public := fs.String("public", "", "clave pública; si no, las incluidas en este ejecutable")
	fs.Parse(args)
	keys, err := pack.TrustedKeys()
	if *public != "" {
		keys, err = pack.ParseKeys(*public)
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(*dir, "manifest.json"))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(filepath.Join(*dir, "manifest.json.sig"))
	if err != nil {
		return err
	}
	m, err := pack.LoadVerified(data, sig, keys)
	if err != nil {
		return err
	}
	fmt.Printf("Firma correcta: %s %s, secuencia %d, %d archivos\n", m.Modpack.Name, m.Modpack.Version, m.Sequence, len(m.Files))
	return nil
}

// publish crea la versión como borrador, sube todo y solo entonces la marca
// como la última: un launcher nunca ve una versión a medias.
func publish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	cfgPath := fs.String("config", "publicacion/publicacion.json", "configuración")
	dir := fs.String("dir", "dist/release", "salida de release")
	fs.Parse(args)
	raw, err := os.ReadFile(*cfgPath)
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	if cfg.GitHub.Owner == "" || cfg.GitHub.Repo == "" {
		return fmt.Errorf("configura github.owner y github.repo en %s", *cfgPath)
	}
	data, err := os.ReadFile(filepath.Join(*dir, "manifest.json"))
	if err != nil {
		return err
	}
	m, err := pack.Parse(data)
	if err != nil {
		return err
	}
	assets, err := filepath.Glob(filepath.Join(*dir, "assets", "*"))
	if err != nil {
		return err
	}
	files := append([]string{filepath.Join(*dir, "manifest.json"), filepath.Join(*dir, "manifest.json.sig")}, assets...)
	for _, c := range publishCommands(cfg.GitHub.Owner+"/"+cfg.GitHub.Repo, m, files) {
		cmd := exec.Command("gh", c...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("gh %s: %w (¿gh instalado y con sesión o GH_TOKEN con permiso contents:write?)", c[1], err)
		}
	}
	fmt.Printf("Publicado. Manifiesto: https://github.com/%s/%s/releases/latest/download/manifest.json\n", cfg.GitHub.Owner, cfg.GitHub.Repo)
	return nil
}

func publishCommands(repo string, m *pack.Manifest, files []string) [][]string {
	tag := fmt.Sprintf("%s-%s-%d", m.Modpack.ID, m.Modpack.Version, m.Sequence)
	notes := "Versión " + m.Modpack.Version + "\n\n- " + strings.Join(m.News, "\n- ")
	cmds := [][]string{{"release", "create", tag, "--repo", repo, "--draft", "--title", m.Modpack.Name + " " + m.Modpack.Version, "--notes", notes}}
	for len(files) > 0 {
		n := min(20, len(files))
		cmds = append(cmds, append([]string{"release", "upload", tag, "--repo", repo, "--clobber"}, files[:n]...))
		files = files[n:]
	}
	return append(cmds, []string{"release", "edit", tag, "--repo", repo, "--draft=false", "--latest"})
}
