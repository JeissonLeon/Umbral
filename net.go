package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Minute}

func getJSON(url string, v any) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s respondió %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Job es un archivo a descargar. Si SHA1 está vacío, basta con que exista.
type Job struct {
	URL  string
	Dest string
	SHA1 string
	// checked indica que ya se comprobó que el archivo falta o no coincide.
	checked bool
}

func (j Job) upToDate() bool {
	if j.SHA1 == "" {
		_, err := os.Stat(j.Dest)
		return err == nil
	}
	h, err := fileSHA1(j.Dest)
	return err == nil && h == j.SHA1
}

func download(j Job) error {
	if !j.checked && j.upToDate() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(j.Dest), 0o755); err != nil {
		return err
	}
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = downloadOnce(j); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return fmt.Errorf("descarga fallida de %s: %w", j.URL, err)
}

func downloadOnce(j Job) error {
	resp, err := httpClient.Get(j.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("respondió %s", resp.Status)
	}
	tmp := j.Dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha1.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); j.SHA1 != "" && got != j.SHA1 {
		os.Remove(tmp)
		return fmt.Errorf("la huella no coincide (esperada %s, recibida %s)", j.SHA1, got)
	}
	return os.Rename(tmp, j.Dest)
}

// downloadAll descarga en paralelo e informa del avance a la interfaz.
func downloadAll(label string, jobs []Job) error {
	if len(jobs) == 0 {
		return nil
	}
	var (
		done     atomic.Int64
		firstErr error
		errOnce  sync.Once
		wg       sync.WaitGroup
	)
	queue := make(chan Job)
	total := len(jobs)
	hub.progress(label, 0, total)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				if err := download(j); err != nil {
					errOnce.Do(func() { firstErr = err })
				}
				hub.progress(label, int(done.Add(1)), total)
			}
		}()
	}
	for _, j := range jobs {
		queue <- j
	}
	close(queue)
	wg.Wait()
	return firstErr
}
