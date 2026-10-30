package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeMicrosoft imita las APIs de Microsoft, Xbox Live y Minecraft.
type fakeMicrosoft struct {
	t          *testing.T
	challenge  string
	noGame     bool
	xstsErr    int64
	refreshes  int
	codeGrants int
}

func (f *fakeMicrosoft) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != "cliente-prueba" || q.Get("code_challenge_method") != "S256" || q.Get("scope") != msScope {
			f.t.Errorf("parámetros de autorización inesperados: %v", q)
		}
		f.challenge = q.Get("code_challenge")
		http.Redirect(w, r, q.Get("redirect_uri")+"/?code=codigo-1&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			f.codeGrants++
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
				f.t.Error("el code_verifier no corresponde al code_challenge")
			}
			if r.Form.Get("code") != "codigo-1" || !strings.HasPrefix(r.Form.Get("redirect_uri"), "http://localhost:") {
				f.t.Errorf("canje de código inesperado: %v", r.Form)
			}
		case "refresh_token":
			f.refreshes++
			if r.Form.Get("refresh_token") != "refresco-1" {
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "caducado"})
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": "ms-acceso", "refresh_token": "refresco-1"})
	})
	mux.HandleFunc("/xbl", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Properties struct{ RpsTicket string } }
		json.NewDecoder(r.Body).Decode(&body)
		if body.Properties.RpsTicket != "d=ms-acceso" {
			f.t.Errorf("RpsTicket inesperado: %q", body.Properties.RpsTicket)
		}
		w.Write([]byte(`{"Token":"xbl-1","DisplayClaims":{"xui":[{"uhs":"uhs-1"}]}}`))
	})
	mux.HandleFunc("/xsts", func(w http.ResponseWriter, r *http.Request) {
		if f.xstsErr != 0 {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]int64{"XErr": f.xstsErr})
			return
		}
		w.Write([]byte(`{"Token":"xsts-1","DisplayClaims":{"xui":[{"uhs":"uhs-1"}]}}`))
	})
	mux.HandleFunc("/mc", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ IdentityToken string }
		json.NewDecoder(r.Body).Decode(&body)
		if body.IdentityToken != "XBL3.0 x=uhs-1;xsts-1" {
			f.t.Errorf("identityToken inesperado: %q", body.IdentityToken)
		}
		w.Write([]byte(`{"access_token":"mc-1","expires_in":86400}`))
	})
	mux.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		if f.noGame {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer mc-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"id":"0123456789abcdef0123456789abcdef","name":"Steve"}`))
	})
	return mux
}

func withFake(t *testing.T) (*fakeMicrosoft, Paths) {
	t.Helper()
	f := &fakeMicrosoft{t: t}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	saved := []string{msAuthorizeURL, msTokenURL, xblURL, xstsURL, mcLoginURL, mcProfileURL}
	savedOpen := openURL
	msAuthorizeURL, msTokenURL = srv.URL+"/authorize", srv.URL+"/token"
	xblURL, xstsURL, mcLoginURL, mcProfileURL = srv.URL+"/xbl", srv.URL+"/xsts", srv.URL+"/mc", srv.URL+"/profile"
	// El "navegador" sigue la redirección hasta el puerto local del launcher.
	openURL = func(u string) {
		go func() {
			resp, err := http.Get(u)
			if err != nil {
				t.Errorf("el navegador simulado falló: %v", err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	t.Cleanup(func() {
		msAuthorizeURL, msTokenURL, xblURL, xstsURL, mcLoginURL, mcProfileURL = saved[0], saved[1], saved[2], saved[3], saved[4], saved[5]
		openURL = savedOpen
	})
	t.Setenv("UMBRAL_HOME", t.TempDir())
	paths, err := newPaths()
	if err != nil {
		t.Fatal(err)
	}
	return f, paths
}

func TestBrowserLoginCompleto(t *testing.T) {
	f, paths := withFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	acc, err := browserLogin(ctx, "cliente-prueba", paths)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Name != "Steve" || acc.AccessToken != "mc-1" || acc.RefreshToken != "refresco-1" {
		t.Fatalf("cuenta inesperada: %+v", acc)
	}
	if f.codeGrants != 1 {
		t.Fatalf("se esperaba un canje de código, hubo %d", f.codeGrants)
	}
	saved := loadAccount(paths)
	if saved == nil || saved.Name != "Steve" {
		t.Fatal("la sesión no se guardó en auth.json")
	}
	if fi, _ := os.Stat(authFile(paths)); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("auth.json no debería ser legible por otros: %v", fi.Mode())
	}
}

func TestSesionCaducadaSeRenueva(t *testing.T) {
	f, paths := withFake(t)
	old := &Account{RefreshToken: "refresco-1", AccessToken: "viejo", Expires: time.Now().Add(-time.Hour)}
	acc, err := ensureSession("cliente-prueba", paths, old)
	if err != nil {
		t.Fatal(err)
	}
	if acc.AccessToken != "mc-1" || f.refreshes != 1 {
		t.Fatalf("no se renovó la sesión: %+v, renovaciones %d", acc, f.refreshes)
	}
}

func TestRefreshInvalidoPideLogin(t *testing.T) {
	_, paths := withFake(t)
	saveAccount(paths, &Account{RefreshToken: "otro"})
	_, err := ensureSession("cliente-prueba", paths, &Account{RefreshToken: "otro", Expires: time.Now().Add(-time.Hour)})
	if !errors.Is(err, errNeedLogin) {
		t.Fatalf("se esperaba errNeedLogin, llegó %v", err)
	}
	if loadAccount(paths) != nil {
		t.Fatal("la sesión inválida debería borrarse")
	}
}

func TestCuentaSinMinecraft(t *testing.T) {
	f, paths := withFake(t)
	f.noGame = true
	_, err := browserLogin(context.Background(), "cliente-prueba", paths)
	if !errors.Is(err, errNoGame) {
		t.Fatalf("se esperaba errNoGame, llegó %v", err)
	}
}

func TestErrorXSTSExplicado(t *testing.T) {
	f, paths := withFake(t)
	f.xstsErr = 2148916233
	_, err := browserLogin(context.Background(), "cliente-prueba", paths)
	if err == nil || !strings.Contains(err.Error(), "perfil de Xbox") {
		t.Fatalf("mensaje inesperado: %v", err)
	}
}

func TestLoginSinClientID(t *testing.T) {
	_, paths := withFake(t)
	if _, err := browserLogin(context.Background(), "", paths); err == nil {
		t.Fatal("sin client ID no debería intentar iniciar sesión")
	}
}

func TestLoginCancelado(t *testing.T) {
	_, paths := withFake(t)
	openURL = func(string) {} // el jugador nunca termina en el navegador
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	if _, err := browserLogin(ctx, "cliente-prueba", paths); err == nil {
		t.Fatal("se esperaba error al cancelar")
	}
}

func TestManifiestoInterno(t *testing.T) {
	m, err := parseManifest(manifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if m.Minecraft != "1.20.1" || len(m.Mods) == 0 {
		t.Fatalf("manifiesto inesperado: %+v", m)
	}
	bad := strings.Replace(string(manifestJSON), `"archivo": "embeddium`, `"archivo": "../embeddium`, 1)
	if _, err := parseManifest([]byte(bad)); err == nil {
		t.Fatal("un nombre de archivo con ruta debería rechazarse")
	}
}

func TestResolveArgsConReglas(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`"--username"`), json.RawMessage(`"${auth_player_name}"`),
		json.RawMessage(`{"rules":[{"action":"allow","features":{"is_demo_user":true}}],"value":"--demo"}`),
		json.RawMessage(`{"rules":[{"action":"allow","features":{"is_quick_play_multiplayer":true}}],"value":["--quickPlayMultiplayer","${quickPlayMultiplayer}"]}`),
	}
	got := resolveArgs(raw, map[string]string{"auth_player_name": "Steve", "quickPlayMultiplayer": "mc.ejemplo.com"}, map[string]bool{"is_quick_play_multiplayer": true})
	want := []string{"--username", "Steve", "--quickPlayMultiplayer", "mc.ejemplo.com"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPingServidor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		n, _ := readVarInt(r)
		io.CopyN(io.Discard, r, int64(n)) // handshake
		readVarInt(r)                     // longitud de la petición
		readVarInt(r)                     // id de la petición
		payload := `{"version":{"name":"1.20.1"},"players":{"max":20,"online":7},"description":{"text":"§aServidor ","extra":[{"text":"Umbral"}]}}`
		var body bytes.Buffer
		writeVarInt(&body, 0)
		writeVarInt(&body, len(payload))
		body.WriteString(payload)
		var out bytes.Buffer
		writeVarInt(&out, body.Len())
		out.Write(body.Bytes())
		c.Write(out.Bytes())
	}()
	st := pingServer(ln.Addr().String())
	if !st.Online || st.Players != 7 || st.Max != 20 || st.Version != "1.20.1" || st.MOTD != "Servidor Umbral" {
		t.Fatalf("estado inesperado: %+v", st)
	}
}

func TestServidorLocalExigeToken(t *testing.T) {
	_, paths := withFake(t)
	a, err := newApp(paths)
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.serve()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.Split(u, "/?")[0]
	resp, _ := http.Get(base + "/api/estado")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sin token debería dar 401, dio %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", base+"/api/estado", nil)
	req.Header.Set("X-Umbral-Token", a.token)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("con token debería dar 200, dio %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", base+"/", nil)
	req.Host = "evil.example:80"
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("un Host ajeno debería dar 403, dio %d", resp.StatusCode)
	}
}

func TestManifiestoRemotoYCopiaLocal(t *testing.T) {
	_, paths := withFake(t)
	remote := strings.Replace(string(manifestJSON), `"version_modpack": "0.1.0"`, `"version_modpack": "9.9.9"`, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(remote)) }))
	if m := loadManifest(paths, srv.URL); m.ModpackVersion != "9.9.9" {
		t.Fatalf("no se usó el manifiesto remoto: %s", m.ModpackVersion)
	}
	srv.Close()
	if m := loadManifest(paths, srv.URL); m.ModpackVersion != "9.9.9" {
		t.Fatalf("sin conexión debería usar la copia local: %s", m.ModpackVersion)
	}
	if m := loadManifest(paths, ""); m.ModpackVersion != "0.1.0" {
		t.Fatalf("sin URL debería usar el interno: %s", m.ModpackVersion)
	}
}
