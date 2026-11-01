package pack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Installer reconcilia la carpeta Root (la instancia del juego) con un
// manifiesto. Su estado vive en Dir, fuera de la instancia pero en el mismo
// disco, para que mover archivos sea atómico.
type Installer struct {
	Root string
	Dir  string
	// Enabled dice si un archivo opcional está activado; nil = ninguno.
	Enabled func(path string) bool
	// OnProgress se llama con el avance de la descarga.
	OnProgress func(Progress)
}

type Progress struct {
	File       string `json:"archivo"`
	FilesDone  int64  `json:"hecho"`
	FilesTotal int    `json:"total"`
	BytesDone  int64  `json:"bytes"`
	BytesTotal int64  `json:"bytes_total"`
	BytesPerS  int64  `json:"velocidad"`
}

// State es lo que hay instalado y es propiedad del launcher. Todo lo que no
// esté aquí se considera del jugador.
type State struct {
	Sequence int64                `json:"sequence"`
	Version  string               `json:"version"`
	TxID     string               `json:"txId"`
	Files    map[string]FileState `json:"files"`
}

type FileState struct {
	SHA256 string `json:"sha256"`
}

// Plan es la diferencia entre lo instalado y lo que pide el manifiesto.
type Plan struct {
	Add     []File   `json:"anadir"`
	Update  []File   `json:"actualizar"`
	Delete  []string `json:"eliminar"`
	Bytes   int64    `json:"bytes"`
	Current int      `json:"correctos"`
}

// Empty indica que no hay nada que cambiar.
func (p *Plan) Empty() bool { return len(p.Add)+len(p.Update)+len(p.Delete) == 0 }

func (in *Installer) statePath() string   { return filepath.Join(in.Dir, "instalacion.json") }
func (in *Installer) journalPath() string { return filepath.Join(in.Dir, "transaccion.json") }
func (in *Installer) stagingDir() string  { return filepath.Join(in.Dir, "descargas") }
func (in *Installer) backupDir() string   { return filepath.Join(in.Dir, "respaldo") }

func (in *Installer) local(p string) string { return filepath.Join(in.Root, filepath.FromSlash(p)) }

// LoadState lee el registro local; sin registro devuelve uno vacío.
func (in *Installer) LoadState() (*State, error) {
	st := &State{Files: map[string]FileState{}}
	data, err := os.ReadFile(in.statePath())
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("registro de instalación dañado: %w", err)
	}
	if st.Files == nil {
		st.Files = map[string]FileState{}
	}
	return st, nil
}

func (in *Installer) wants(f File) bool {
	return f.Required || (in.Enabled != nil && in.Enabled(f.Path))
}

// Plan compara el disco real con el manifiesto. Calcula el hash de cada
// archivo, así que también detecta los modificados o corruptos.
func (in *Installer) Plan(m *Manifest) (*Plan, error) {
	st, err := in.LoadState()
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	wanted := map[string]bool{}
	for _, f := range m.Files {
		if !in.wants(f) {
			continue
		}
		wanted[f.Path] = true
		h, err := HashFile(in.local(f.Path))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			plan.Add = append(plan.Add, f)
			plan.Bytes += f.Size
		case err != nil:
			return nil, fmt.Errorf("no se pudo leer %s: %w", f.Path, err)
		case h == f.SHA256:
			plan.Current++
		case f.policy() == PolicySeed && st.Files[f.Path].SHA256 == f.SHA256:
			plan.Current++ // el jugador lo editó y el manifiesto no ha cambiado
		default:
			plan.Update = append(plan.Update, f)
			plan.Bytes += f.Size
		}
	}
	deleted := map[string]bool{}
	del := func(p string) {
		if _, err := os.Lstat(in.local(p)); err == nil && !deleted[p] {
			deleted[p] = true
			plan.Delete = append(plan.Delete, p)
		}
	}
	for p := range st.Files {
		if !wanted[p] {
			del(p)
		}
	}
	for _, r := range m.Remove {
		if wanted[r.Path] {
			continue
		}
		_, tracked := st.Files[r.Path]
		if !tracked && r.SHA256 != "" {
			if h, err := HashFile(in.local(r.Path)); err != nil || h != r.SHA256 {
				continue
			}
		} else if !tracked {
			continue
		}
		del(r.Path)
	}
	return plan, nil
}

// journal describe una transacción en curso, para deshacerla si se interrumpe.
type journal struct {
	TxID string `json:"txId"`
	Ops  []txOp `json:"ops"`
}

type txOp struct {
	Path   string `json:"path"`
	Backup string `json:"backup,omitempty"` // vacío: el archivo no existía antes
}

// Recover deshace una transacción interrumpida (corte de luz, cierre forzado).
// Si el registro ya se había escrito, la transacción terminó y solo limpia.
func (in *Installer) Recover() error {
	data, err := os.ReadFile(in.journalPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var j journal
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("transacción dañada: %w", err)
	}
	st, err := in.LoadState()
	if err != nil {
		return err
	}
	if st.TxID != j.TxID {
		log.Printf("Deshaciendo una actualización interrumpida (%s)", j.TxID)
		if err := in.rollback(j.Ops); err != nil {
			return err
		}
	}
	return in.finishTx()
}

func (in *Installer) rollback(ops []txOp) error {
	var first error
	for i := len(ops) - 1; i >= 0; i-- {
		op := ops[i]
		dest := in.local(op.Path)
		if op.Backup == "" {
			if err := os.Remove(dest); err != nil && !errors.Is(err, fs.ErrNotExist) && first == nil {
				first = err
			}
			continue
		}
		if _, err := os.Stat(op.Backup); err != nil {
			continue // el respaldo aún no se hizo: el original sigue en su sitio
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil && first == nil {
			first = err
		}
		if err := os.Rename(op.Backup, dest); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (in *Installer) finishTx() error {
	if err := os.Remove(in.journalPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.RemoveAll(in.backupDir())
}

// Apply descarga y verifica todo lo necesario y solo entonces toca la
// instancia. Si algo falla al aplicar, deja la instalación como estaba. El
// registro se actualiza al final: hasta entonces no consta como actualizada.
func (in *Installer) Apply(ctx context.Context, m *Manifest, plan *Plan) error {
	if err := os.MkdirAll(in.Dir, 0o755); err != nil {
		return permissionHint(in.Dir, err)
	}
	if err := os.MkdirAll(in.Root, 0o755); err != nil {
		return permissionHint(in.Root, err)
	}
	if err := in.Recover(); err != nil {
		return err
	}
	staged, skipped, err := in.download(ctx, plan)
	if err != nil {
		return err
	}
	st, err := in.LoadState()
	if err != nil {
		return err
	}

	txID := fmt.Sprintf("%d-%d", m.Sequence, time.Now().UnixNano())
	backup := in.backupDir()
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return permissionHint(backup, err)
	}
	type step struct {
		op     txOp
		staged string
	}
	var steps []step
	n := 0
	backupFor := func() string { n++; return filepath.Join(backup, fmt.Sprint(n)) }
	plannedFiles := append(append([]File{}, plan.Add...), plan.Update...)
	for _, f := range plannedFiles {
		src, ok := staged[f.Path]
		if !ok {
			continue
		}
		op := txOp{Path: f.Path}
		if _, err := os.Lstat(in.local(f.Path)); err == nil {
			op.Backup = backupFor()
		}
		steps = append(steps, step{op, src})
	}
	for _, p := range plan.Delete {
		steps = append(steps, step{txOp{Path: p, Backup: backupFor()}, ""})
	}
	j := journal{TxID: txID}
	for _, s := range steps {
		j.Ops = append(j.Ops, s.op)
	}
	if err := writeJSONAtomic(in.journalPath(), j); err != nil {
		return permissionHint(in.Dir, err)
	}

	apply := func(s step) error {
		dest := in.local(s.op.Path)
		if s.op.Backup != "" {
			if err := os.Rename(dest, s.op.Backup); err != nil {
				return err
			}
		}
		if s.staged == "" {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.Rename(s.staged, dest)
	}
	for _, s := range steps {
		if err := apply(s); err != nil {
			if rerr := in.rollback(j.Ops); rerr != nil {
				return fmt.Errorf("falló la actualización (%v) y también deshacerla: %w", err, rerr)
			}
			in.finishTx()
			return permissionHint(in.local(s.op.Path), err)
		}
	}

	next := &State{Sequence: m.Sequence, Version: m.Modpack.Version, TxID: txID, Files: map[string]FileState{}}
	for _, f := range m.Files {
		if !in.wants(f) {
			continue
		}
		if skipped[f.Path] {
			if old, ok := st.Files[f.Path]; ok {
				next.Files[f.Path] = old
			}
			continue
		}
		next.Files[f.Path] = FileState{SHA256: f.SHA256}
	}
	if err := writeJSONAtomic(in.statePath(), next); err != nil {
		if rerr := in.rollback(j.Ops); rerr != nil {
			return fmt.Errorf("no se pudo guardar el registro (%v) ni deshacer: %w", err, rerr)
		}
		in.finishTx()
		return permissionHint(in.Dir, err)
	}
	if err := in.finishTx(); err != nil {
		log.Println("No se pudo limpiar tras actualizar:", err)
	}
	os.RemoveAll(in.stagingDir())
	return nil
}

// download baja los archivos del plan. Un opcional que falla no detiene nada:
// se omite y se avisa. Un obligatorio detiene todo antes de tocar la instancia.
func (in *Installer) download(ctx context.Context, plan *Plan) (staged map[string]string, skipped map[string]bool, err error) {
	files := append(append([]File{}, plan.Add...), plan.Update...)
	staged, skipped = map[string]string{}, map[string]bool{}
	if len(files) == 0 {
		return staged, skipped, nil
	}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		bytes    atomic.Int64
		done     atomic.Int64
		last     atomic.Int64
		firstErr error
	)
	start := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	notify := func(name string, force bool) {
		if in.OnProgress == nil {
			return
		}
		now := time.Now().UnixMilli()
		if prev := last.Load(); !force && now-prev < 100 {
			return
		}
		last.Store(now)
		b := bytes.Load()
		speed := int64(float64(b) / max(time.Since(start).Seconds(), 0.1))
		in.OnProgress(Progress{File: name, FilesDone: done.Load(), FilesTotal: len(files), BytesDone: b, BytesTotal: plan.Bytes, BytesPerS: speed})
	}
	queue := make(chan File)
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range queue {
				dest := filepath.Join(in.stagingDir(), f.SHA256)
				err := Fetch(ctx, f.URL, dest, f.SHA256, f.Size, func(n int64) {
					bytes.Add(n)
					notify(f.Path, false)
				})
				done.Add(1)
				notify(f.Path, true)
				mu.Lock()
				switch {
				case err == nil:
					staged[f.Path] = dest
				case !f.Required && ctx.Err() == nil:
					skipped[f.Path] = true
					log.Printf("Opcional omitido, %s: %v", f.Path, err)
				default:
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", f.Path, err)
						cancel()
					}
				}
				mu.Unlock()
			}
		}()
	}
	queued := map[string]bool{}
	for _, f := range files {
		if !queued[f.SHA256] { // mismo contenido, una sola descarga
			queued[f.SHA256] = true
			queue <- f
		}
	}
	close(queue)
	wg.Wait()
	if firstErr == nil {
		for _, f := range files {
			if _, ok := staged[f.Path]; !ok && !skipped[f.Path] {
				staged[f.Path] = filepath.Join(in.stagingDir(), f.SHA256)
			}
		}
	}
	if firstErr != nil {
		return nil, nil, firstErr
	}
	// Dos rutas con el mismo contenido comparten descarga: la segunda necesita copia.
	seen := map[string]bool{}
	for _, f := range files {
		src, ok := staged[f.Path]
		if !ok {
			continue
		}
		if seen[src] {
			cp := src + "." + fmt.Sprint(len(seen))
			data, err := os.ReadFile(src)
			if err != nil {
				return nil, nil, err
			}
			if err := os.WriteFile(cp, data, 0o644); err != nil {
				return nil, nil, err
			}
			staged[f.Path] = cp
		}
		seen[src] = true
	}
	return staged, skipped, nil
}

func writeJSONAtomic(p string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func permissionHint(p string, err error) error {
	if errors.Is(err, fs.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "access is denied") {
		return fmt.Errorf("sin permiso de escritura en %s (o un programa lo tiene abierto); cierra Minecraft o elige otra carpeta en Ajustes: %w", p, err)
	}
	return err
}
