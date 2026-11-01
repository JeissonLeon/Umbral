package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrCorrupt indica que lo descargado no coincide con el manifiesto.
var ErrCorrupt = errors.New("el archivo descargado no coincide con el manifiesto")

// Client no tiene tiempo total (los archivos pueden ser grandes); fetchOnce
// corta si la conexión deja de entregar datos. No sigue redirecciones a HTTP.
var Client = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("demasiadas redirecciones")
		}
		return CheckURL(req.URL.String())
	},
}

// Ajustes de reintento; variables para que las pruebas no esperen de verdad.
var (
	StallTimeout = 45 * time.Second
	RetryDelay   = 2 * time.Second
	MaxAttempts  = 5
)

// HashFile devuelve el SHA-256 de un archivo.
func HashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Fetch deja en dest el archivo verificado. Descarga en dest.part y continúa
// desde ahí si una ejecución anterior se cortó. onBytes recibe lo que avanza
// (negativo si hay que descartar una parte).
func Fetch(ctx context.Context, rawURL, dest, sha string, size int64, onBytes func(int64)) error {
	if onBytes == nil {
		onBytes = func(int64) {}
	}
	if h, err := HashFile(dest); err == nil && h == sha {
		onBytes(size)
		return nil
	}
	if err := CheckURL(rawURL); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	reported := int64(0)
	report := func(n int64) { reported += n; onBytes(n) }
	if fi, err := os.Stat(part); err == nil {
		report(fi.Size())
	}
	var err error
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		if err = fetchOnce(ctx, rawURL, part, size, report); err == nil {
			var h string
			if h, err = HashFile(part); err != nil {
				return err
			}
			if h == sha {
				return os.Rename(part, dest)
			}
			err = fmt.Errorf("%w (esperado %s, recibido %s)", ErrCorrupt, sha[:12], h[:12])
		}
		var perm permanentError
		if errors.As(err, &perm) || ctx.Err() != nil {
			break
		}
		if errors.Is(err, ErrCorrupt) {
			os.Remove(part)
			report(-reported)
		}
		select {
		case <-ctx.Done():
		case <-time.After(RetryDelay * time.Duration(attempt)):
		}
	}
	if errors.Is(err, ErrCorrupt) {
		os.Remove(part)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type permanentError struct{ error }

func (e permanentError) Unwrap() error { return e.error }

func fetchOnce(parent context.Context, rawURL, part string, size int64, report func(int64)) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	var have int64
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
	}
	if have > size {
		report(-have)
		os.Remove(part)
		have = 0
	}
	if have == size {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "UmbralLauncher")
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	watchdog := time.AfterFunc(StallTimeout, cancel)
	defer watchdog.Stop()
	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusPartialContent:
		if !strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", have)) {
			return fmt.Errorf("el servidor devolvió un rango inesperado")
		}
		flags |= os.O_APPEND
	case http.StatusOK:
		report(-have)
		have = 0
		flags |= os.O_TRUNC
	case http.StatusRequestedRangeNotSatisfiable:
		report(-have)
		os.Remove(part)
		return errors.New("el servidor rechazó continuar la descarga")
	case http.StatusNotFound, http.StatusForbidden, http.StatusGone, http.StatusUnauthorized:
		return permanentError{fmt.Errorf("%s respondió %s", rawURL, resp.Status)}
	default:
		return fmt.Errorf("%s respondió %s", rawURL, resp.Status)
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return permanentError{err}
	}
	defer f.Close()

	buf := make([]byte, 64*1024)
	for {
		watchdog.Reset(StallTimeout)
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			have += int64(n)
			if have > size {
				report(-(have - int64(n)))
				f.Close()
				os.Remove(part)
				return fmt.Errorf("%w (más grande de lo esperado)", ErrCorrupt)
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return permanentError{err}
			}
			report(int64(n))
		}
		if rerr == io.EOF {
			if have < size {
				return fmt.Errorf("la descarga terminó antes de tiempo (%d de %d bytes)", have, size)
			}
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}
