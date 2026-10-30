package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ServerStatus es lo que responde un servidor de Minecraft a la consulta
// de estado (la misma que usa la lista de servidores del juego).
type ServerStatus struct {
	Online    bool   `json:"en_linea"`
	Players   int    `json:"jugadores"`
	Max       int    `json:"maximo"`
	Version   string `json:"version"`
	MOTD      string `json:"motd"`
	LatencyMS int64  `json:"latencia_ms"`
	Error     string `json:"error,omitempty"`
}

// splitServer separa host y puerto. Sin puerto, busca el registro SRV como
// hace el juego y si no usa 25565.
func splitServer(addr string) (host string, port int, err error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0, errors.New("dirección vacía")
	}
	if h, p, err := net.SplitHostPort(addr); err == nil {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", 0, errors.New("puerto no válido")
		}
		return h, n, nil
	}
	if strings.ContainsAny(addr, " /\\") {
		return "", 0, errors.New("dirección no válida")
	}
	if _, srvs, err := net.LookupSRV("minecraft", "tcp", addr); err == nil && len(srvs) > 0 {
		return strings.TrimSuffix(srvs[0].Target, "."), int(srvs[0].Port), nil
	}
	return addr, 25565, nil
}

func pingServer(addr string) ServerStatus {
	host, port, err := splitServer(addr)
	if err != nil {
		return ServerStatus{Error: err.Error()}
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		return ServerStatus{Error: "no responde"}
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(6 * time.Second))

	var hs bytes.Buffer
	writeVarInt(&hs, 0)   // id del paquete handshake
	writeVarInt(&hs, 763) // protocolo de 1.20.1
	writeVarInt(&hs, len(host))
	hs.WriteString(host)
	binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarInt(&hs, 1) // siguiente estado: status
	var out bytes.Buffer
	writeVarInt(&out, hs.Len())
	out.Write(hs.Bytes())
	out.Write([]byte{1, 0}) // petición de estado
	if _, err := conn.Write(out.Bytes()); err != nil {
		return ServerStatus{Error: "no responde"}
	}

	r := bufio.NewReader(conn)
	if _, err := readVarInt(r); err != nil { // longitud
		return ServerStatus{Error: "respuesta no válida"}
	}
	if id, err := readVarInt(r); err != nil || id != 0 {
		return ServerStatus{Error: "respuesta no válida"}
	}
	n, err := readVarInt(r)
	if err != nil || n <= 0 || n > 1<<20 {
		return ServerStatus{Error: "respuesta no válida"}
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return ServerStatus{Error: "respuesta no válida"}
	}
	latency := time.Since(start).Milliseconds()

	var resp struct {
		Version struct {
			Name string `json:"name"`
		} `json:"version"`
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
		} `json:"players"`
		Description json.RawMessage `json:"description"`
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return ServerStatus{Error: "respuesta no válida"}
	}
	return ServerStatus{
		Online:    true,
		Players:   resp.Players.Online,
		Max:       resp.Players.Max,
		Version:   resp.Version.Name,
		MOTD:      chatText(resp.Description),
		LatencyMS: latency,
	}
}

var formatCodes = regexp.MustCompile(`§.`)

// chatText aplana un componente de texto de Minecraft a texto plano.
func chatText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(formatCodes.ReplaceAllString(s, ""))
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(chatText(p))
		}
		return b.String()
	}
	var obj struct {
		Text  string            `json:"text"`
		Extra []json.RawMessage `json:"extra"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(obj.Text)
	for _, e := range obj.Extra {
		b.WriteString(chatText(e))
	}
	return strings.TrimSpace(formatCodes.ReplaceAllString(b.String(), ""))
}

func writeVarInt(w *bytes.Buffer, v int) {
	u := uint32(v)
	for {
		if u&^0x7f == 0 {
			w.WriteByte(byte(u))
			return
		}
		w.WriteByte(byte(u&0x7f | 0x80))
		u >>= 7
	}
}

func readVarInt(r io.ByteReader) (int, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int(int32(v)), nil
		}
	}
	return 0, fmt.Errorf("varint demasiado largo")
}
