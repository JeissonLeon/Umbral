package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// Hub reparte eventos a la interfaz por Server-Sent Events.
type Hub struct {
	mu           sync.Mutex
	subs         map[chan []byte]struct{}
	lastProgress time.Time
}

var hub = &Hub{subs: map[chan []byte]struct{}{}}

func (h *Hub) subscribe() chan []byte {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *Hub) send(kind string, data any) {
	msg, _ := json.Marshal(map[string]any{"tipo": kind, "datos": data})
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // la interfaz va atrasada; se descarta este evento
		}
	}
}

// stage anuncia un paso nuevo de la preparación.
func (h *Hub) stage(text string) {
	log.Println(text)
	h.send("paso", map[string]string{"texto": text})
}

// progress informa del avance de una descarga, como mucho diez veces por
// segundo salvo al terminar.
func (h *Hub) progress(label string, done, total int) {
	h.mu.Lock()
	now := time.Now()
	if done < total && now.Sub(h.lastProgress) < 100*time.Millisecond {
		h.mu.Unlock()
		return
	}
	h.lastProgress = now
	h.mu.Unlock()
	h.send("progreso", map[string]any{"etiqueta": label, "hecho": done, "total": total})
}
