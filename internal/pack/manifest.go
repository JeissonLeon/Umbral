// Package pack define el manifiesto firmado del modpack y cómo reconciliar una
// instalación con él. Lo usan el launcher, el actualizador y el publicador.
package pack

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
)

// SchemaVersion es la versión del formato que entiende este launcher.
const SchemaVersion = 1

// Políticas de un archivo administrado.
const (
	// PolicyManaged: si el archivo local difiere del manifiesto, se restaura.
	PolicyManaged = "managed"
	// PolicySeed: se instala si falta o si el manifiesto trae otra versión; los
	// cambios del jugador se respetan mientras el manifiesto no cambie.
	PolicySeed = "seed"
)

// AllowedRoots son las carpetas de la instancia que puede tocar un manifiesto.
// Ni saves ni screenshots están aquí: un manifiesto no puede tocar los mundos.
var AllowedRoots = []string{"mods", "config", "defaultconfigs", "resourcepacks", "shaderpacks", "kubejs", "scripts", "patchouli_books"}

type Manifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	Sequence      int64    `json:"sequence"`
	PublishedAt   string   `json:"publishedAt"`
	Modpack       Modpack  `json:"modpack"`
	Launcher      Launcher `json:"launcher"`
	UmbralMod     Umbral   `json:"umbralMod"`
	News          []string `json:"news,omitempty"`
	Files         []File   `json:"files"`
	Remove        []Remove `json:"remove"`
}

type Modpack struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Minecraft string `json:"minecraft"`
	Forge     string `json:"forge"`
	Java      int    `json:"java"`
	// ForgeInstallerSHA256 fija el instalador oficial de Forge.
	ForgeInstallerSHA256 string `json:"forgeInstallerSha256,omitempty"`
}

type Launcher struct {
	Version string  `json:"version"`
	Files   []Asset `json:"files,omitempty"`
}

// Asset es un ejecutable del launcher. Su autenticidad viene de la firma del
// manifiesto que contiene su hash.
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Umbral struct {
	Version string `json:"version"`
	Path    string `json:"path"`
}

type File struct {
	Path        string `json:"path"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	Required    bool   `json:"required"`
	Policy      string `json:"policy,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// Remove es un archivo retirado. Se borra si el launcher lo tiene registrado o
// si su hash coincide; si no, puede ser del jugador y se deja.
type Remove struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
}

func (f File) policy() string {
	if f.Policy == "" {
		return PolicyManaged
	}
	return f.Policy
}

// Parse decodifica y valida un manifiesto. No comprueba la firma: eso es Verify.
func Parse(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifiesto no válido: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("manifiesto no válido: %w", err)
	}
	return &m, nil
}

func (m *Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("versión de esquema %d no soportada; actualiza el launcher", m.SchemaVersion)
	}
	if m.Sequence < 1 {
		return errors.New("sequence debe ser mayor que 0")
	}
	p := m.Modpack
	if p.ID == "" || p.Version == "" || p.Minecraft == "" || p.Forge == "" || p.Java == 0 {
		return errors.New("faltan datos del modpack (id, version, minecraft, forge o java)")
	}
	if p.ForgeInstallerSHA256 != "" && !validSHA256(p.ForgeInstallerSHA256) {
		return errors.New("forgeInstallerSha256 no es un SHA-256")
	}
	if m.Launcher.Version == "" {
		return errors.New("falta la versión del launcher")
	}
	for _, a := range m.Launcher.Files {
		if a.Name == "" || a.Name != path.Base(a.Name) || strings.ContainsAny(a.Name, `\:`) {
			return fmt.Errorf("nombre de ejecutable inseguro: %q", a.Name)
		}
		if err := validateDownload(a.URL, a.SHA256, a.Size); err != nil {
			return fmt.Errorf("launcher %s: %w", a.Name, err)
		}
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		if err := SafePath(f.Path); err != nil {
			return err
		}
		key := strings.ToLower(f.Path)
		if seen[key] {
			return fmt.Errorf("ruta duplicada: %s", f.Path)
		}
		seen[key] = true
		if err := validateDownload(f.URL, f.SHA256, f.Size); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		if f.Policy != "" && f.Policy != PolicyManaged && f.Policy != PolicySeed {
			return fmt.Errorf("%s: política desconocida %q", f.Path, f.Policy)
		}
	}
	for _, r := range m.Remove {
		if err := SafePath(r.Path); err != nil {
			return err
		}
		if seen[strings.ToLower(r.Path)] {
			return fmt.Errorf("%s está a la vez en files y en remove", r.Path)
		}
		if r.SHA256 != "" && !validSHA256(r.SHA256) {
			return fmt.Errorf("%s: hash de remove no válido", r.Path)
		}
	}
	if m.UmbralMod.Path != "" && !seen[strings.ToLower(m.UmbralMod.Path)] {
		return fmt.Errorf("umbralMod.path %s no está en files", m.UmbralMod.Path)
	}
	return nil
}

func validateDownload(rawURL, sha string, size int64) error {
	if !validSHA256(sha) {
		return errors.New("SHA-256 no válido")
	}
	if size <= 0 {
		return errors.New("tamaño no válido")
	}
	return CheckURL(rawURL)
}

// CheckURL exige HTTPS. Solo se acepta HTTP hacia la propia máquina, para
// probar sin publicar; un manifiesto firmado sigue siendo necesario.
func CheckURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("URL no válida: %q", rawURL)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
			return nil
		}
	}
	return fmt.Errorf("solo se admite HTTPS: %q", rawURL)
}

func validSHA256(s string) bool {
	if len(s) != 64 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

var windowsReserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

func init() {
	for _, n := range "123456789" {
		windowsReserved["com"+string(n)] = true
		windowsReserved["lpt"+string(n)] = true
	}
}

// SafePath acepta solo rutas relativas con barras normales, dentro de una
// carpeta permitida y sin nada que Windows interprete de otra forma.
func SafePath(p string) error {
	bad := fmt.Errorf("ruta insegura: %q", p)
	if p == "" || strings.ContainsAny(p, "\\:*?\"<>|\x00") || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return bad
	}
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return bad
	}
	for _, s := range parts {
		base, _, _ := strings.Cut(strings.ToLower(s), ".")
		if s == ".." || s == "." || strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") || windowsReserved[base] {
			return bad
		}
	}
	for _, root := range AllowedRoots {
		if parts[0] == root {
			return nil
		}
	}
	return fmt.Errorf("la carpeta %q no la puede administrar un manifiesto", parts[0])
}
