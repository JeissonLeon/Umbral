package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const versionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type Artifact struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

type Rule struct {
	Action string `json:"action"`
	OS     struct {
		Name string `json:"name"`
		Arch string `json:"arch"`
	} `json:"os"`
	Features map[string]bool `json:"features"`
}

type Library struct {
	Name      string `json:"name"`
	Downloads struct {
		Artifact    *Artifact           `json:"artifact"`
		Classifiers map[string]Artifact `json:"classifiers"`
	} `json:"downloads"`
	Natives map[string]string `json:"natives"`
	Rules   []Rule            `json:"rules"`
}

type VersionJSON struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
	Type         string `json:"type"`
	MainClass    string `json:"mainClass"`
	Arguments    struct {
		Game []json.RawMessage `json:"game"`
		JVM  []json.RawMessage `json:"jvm"`
	} `json:"arguments"`
	Libraries  []Library `json:"libraries"`
	AssetIndex struct {
		ID   string `json:"id"`
		URL  string `json:"url"`
		SHA1 string `json:"sha1"`
	} `json:"assetIndex"`
	Downloads struct {
		Client Artifact `json:"client"`
	} `json:"downloads"`
	JavaVersion struct {
		Component    string `json:"component"`
		MajorVersion int    `json:"majorVersion"`
	} `json:"javaVersion"`
}

func mojangOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "osx"
	default:
		return runtime.GOOS
	}
}

// rulesAllow aplica las reglas de Mojang: sin reglas se permite; con
// reglas, gana la última que coincide.
func rulesAllow(rules []Rule, features map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		if r.OS.Name != "" && r.OS.Name != mojangOS() {
			continue
		}
		if r.OS.Arch == "x86" && runtime.GOARCH != "386" {
			continue
		}
		match := true
		for f, want := range r.Features {
			if features[f] != want {
				match = false
			}
		}
		if match {
			allowed = r.Action == "allow"
		}
	}
	return allowed
}

func readVersion(paths Paths, id string) (*VersionJSON, error) {
	data, err := os.ReadFile(filepath.Join(paths.Versions, id, id+".json"))
	if err != nil {
		return nil, err
	}
	var v VersionJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("versión %s dañada: %w", id, err)
	}
	return &v, nil
}

// libArtifact devuelve el archivo de una librería para este sistema, si
// aplica, y si es una librería nativa.
func libArtifact(lib Library) (*Artifact, bool) {
	if !rulesAllow(lib.Rules, nil) {
		return nil, false
	}
	if lib.Natives != nil {
		classifier, ok := lib.Natives[mojangOS()]
		if !ok {
			return nil, false
		}
		arch := "64"
		if runtime.GOARCH == "386" {
			arch = "32"
		}
		a, ok := lib.Downloads.Classifiers[strings.ReplaceAll(classifier, "${arch}", arch)]
		if !ok {
			return nil, false
		}
		return &a, true
	}
	if lib.Downloads.Artifact == nil {
		return nil, false
	}
	return lib.Downloads.Artifact, strings.Contains(lib.Name, ":natives-")
}

func ensureVanilla(paths Paths, mcVersion string) (*VersionJSON, error) {
	hub.stage("Comprobando Minecraft " + mcVersion)
	dir := filepath.Join(paths.Versions, mcVersion)
	jsonPath := filepath.Join(dir, mcVersion+".json")

	if _, err := os.Stat(jsonPath); err != nil {
		var manifest struct {
			Versions []struct {
				ID   string `json:"id"`
				URL  string `json:"url"`
				SHA1 string `json:"sha1"`
			} `json:"versions"`
		}
		if err := getJSON(versionManifestURL, &manifest); err != nil {
			return nil, fmt.Errorf("no se pudo leer la lista de versiones de Mojang: %w", err)
		}
		found := false
		for _, v := range manifest.Versions {
			if v.ID == mcVersion {
				if err := download(Job{URL: v.URL, Dest: jsonPath, SHA1: v.SHA1}); err != nil {
					return nil, err
				}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("Mojang no tiene la versión %s", mcVersion)
		}
	}
	v, err := readVersion(paths, mcVersion)
	if err != nil {
		return nil, err
	}

	client := v.Downloads.Client
	jobs := []Job{{URL: client.URL, Dest: filepath.Join(dir, mcVersion+".jar"), SHA1: client.SHA1}}
	for _, lib := range v.Libraries {
		if a, _ := libArtifact(lib); a != nil {
			jobs = append(jobs, Job{URL: a.URL, Dest: filepath.Join(paths.Libraries, a.Path), SHA1: a.SHA1})
		}
	}
	if err := downloadAll("Juego y librerías", pending(jobs)); err != nil {
		return nil, err
	}
	if err := ensureAssets(paths, v); err != nil {
		return nil, err
	}
	return v, nil
}

func ensureAssets(paths Paths, v *VersionJSON) error {
	indexPath := filepath.Join(paths.Assets, "indexes", v.AssetIndex.ID+".json")
	if err := download(Job{URL: v.AssetIndex.URL, Dest: indexPath, SHA1: v.AssetIndex.SHA1}); err != nil {
		return err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	var index struct {
		Objects map[string]struct {
			Hash string `json:"hash"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("índice de recursos dañado: %w", err)
	}
	var jobs []Job
	seen := map[string]bool{}
	for _, o := range index.Objects {
		if seen[o.Hash] {
			continue
		}
		seen[o.Hash] = true
		sub := o.Hash[:2] + "/" + o.Hash
		dest := filepath.Join(paths.Assets, "objects", o.Hash[:2], o.Hash)
		// Los recursos ya descargados se dan por buenos: verificar miles de
		// huellas en cada arranque lo haría lento.
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		jobs = append(jobs, Job{URL: "https://resources.download.minecraft.net/" + sub, Dest: dest, SHA1: o.Hash})
	}
	return downloadAll("Sonidos e idiomas", jobs)
}

// pending deja solo los trabajos cuyo archivo falta o no coincide, para que
// la barra de progreso refleje lo que de verdad se descarga.
func pending(jobs []Job) []Job {
	var out []Job
	for _, j := range jobs {
		if !j.upToDate() {
			j.checked = true
			out = append(out, j)
		}
	}
	return out
}
