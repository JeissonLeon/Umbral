package main

import (
	"crypto/ed25519"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"umbral/internal/pack"
)

// signedManifest es un manifiesto ya verificado junto con sus bytes firmados.
type signedManifest struct {
	*pack.Manifest
	raw, sig []byte
}

func manifestCache(paths Paths) (string, string) {
	base := filepath.Join(paths.Root, "manifiesto-verificado.json")
	return base, base + ".sig"
}

// cachedManifest devuelve la última copia verificada; la firma se comprueba
// otra vez porque el disco no es de confianza. nil si no hay ninguna válida.
func cachedManifest(paths Paths, keys []ed25519.PublicKey) *signedManifest {
	mPath, sPath := manifestCache(paths)
	raw, err := os.ReadFile(mPath)
	if err != nil {
		return nil
	}
	sig, err := os.ReadFile(sPath)
	if err != nil {
		return nil
	}
	m, err := pack.LoadVerified(raw, sig, keys)
	if err != nil {
		return nil
	}
	return &signedManifest{m, raw, sig}
}

// fetchManifest descarga manifiesto y firma, verifica y lo guarda. Rechaza una
// secuencia menor que la ya vista, para que nadie pueda servir una versión
// antigua y vulnerable aunque esté bien firmada.
func fetchManifest(paths Paths, url string, keys []ed25519.PublicKey) (*signedManifest, error) {
	raw, err := fetchBytes(url)
	if err != nil {
		return nil, err
	}
	sig, err := fetchBytes(url + ".sig")
	if err != nil {
		return nil, err
	}
	m, err := pack.LoadVerified(raw, sig, keys)
	if err != nil {
		return nil, err
	}
	if old := cachedManifest(paths, keys); old != nil && m.Sequence < old.Sequence {
		return nil, fmt.Errorf("el manifiesto publicado (secuencia %d) es más antiguo que el ya aplicado (%d)", m.Sequence, old.Sequence)
	}
	mPath, sPath := manifestCache(paths)
	if err := os.WriteFile(sPath, sig, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(mPath, raw, 0o644); err != nil {
		return nil, err
	}
	return &signedManifest{m, raw, sig}, nil
}

func fetchBytes(url string) ([]byte, error) {
	if err := pack.CheckURL(url); err != nil {
		return nil, err
	}
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s respondió %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}
