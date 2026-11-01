package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Variables y no constantes solo para que los tests apunten a un servidor
// falso; no hay forma de cambiarlas desde fuera del código.
var (
	msAuthorizeURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize"
	msTokenURL     = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	xblURL         = "https://user.auth.xboxlive.com/user/authenticate"
	xstsURL        = "https://xsts.auth.xboxlive.com/xsts/authorize"
	mcLoginURL     = "https://api.minecraftservices.com/authentication/login_with_xbox"
	mcProfileURL   = "https://api.minecraftservices.com/minecraft/profile"
	// openURL abre el navegador del sistema; los tests lo sustituyen.
	openURL = openPath
)

const msScope = "XboxLive.signin offline_access"

var (
	errNeedLogin     = errors.New("inicia sesión con tu cuenta de Microsoft para jugar")
	errLoginCanceled = errors.New("inicio de sesión cancelado")
)

// Account es la sesión de Minecraft del modo de inicio directo. Solo existe en
// memoria mientras el launcher está abierto: nunca se escribe a disco.
type Account struct {
	RefreshToken string
	AccessToken  string
	Expires      time.Time
	UUID         string
	Name         string
}

// ensureSession devuelve una sesión válida para jugar, renovándola con el
// refresh token si caducó. Comprueba también que la cuenta siga teniendo
// Minecraft Java.
func ensureSession(clientID string, acc *Account) (*Account, error) {
	if acc == nil {
		return nil, errNeedLogin
	}
	if time.Until(acc.Expires) > 10*time.Minute {
		name, uuid, err := fetchProfile(acc.AccessToken)
		if err == nil {
			acc.Name, acc.UUID = name, uuid
			return acc, nil
		}
		if errors.Is(err, errNoGame) {
			return nil, err
		}
	}
	ms, err := msTokenRequest(url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {acc.RefreshToken},
		"scope":         {msScope},
	})
	if err != nil {
		log.Println("No se pudo renovar la sesión:", err)
		return nil, errNeedLogin
	}
	return minecraftFromMicrosoft(ms)
}

// browserLogin abre la página de Microsoft en el navegador del sistema y
// recibe la respuesta en un puerto local (OAuth con PKCE). El launcher nunca
// ve la contraseña.
func browserLogin(ctx context.Context, clientID string) (*Account, error) {
	if clientID == "" {
		return nil, errors.New("este launcher se compiló sin ID de aplicación de Microsoft; mira LEEME.md")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	// Algunos navegadores resuelven "localhost" a ::1 primero.
	if ln6, err := net.Listen("tcp", fmt.Sprintf("[::1]:%d", port)); err == nil {
		defer ln6.Close()
		go serveCallback(ln6)
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://localhost:%d", port)

	verifier := randomString(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomString(16)

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	callbackHandler = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "Petición no válida", http.StatusBadRequest)
			return
		}
		var res result
		if e := q.Get("error"); e != "" {
			res.err = fmt.Errorf("Microsoft no completó el inicio de sesión: %s", q.Get("error_description"))
			writeCallbackPage(w, false, "No se completó el inicio de sesión. Puedes volver al launcher e intentarlo de nuevo.")
		} else {
			res.code = q.Get("code")
			writeCallbackPage(w, true, "Sesión iniciada. Ya puedes cerrar esta pestaña y volver al launcher.")
		}
		select {
		case results <- res:
		default:
		}
	}
	go serveCallback(ln)

	authURL := msAuthorizeURL + "?" + url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirect},
		"scope":                 {msScope},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"prompt":                {"select_account"},
	}.Encode()
	openURL(authURL)

	var res result
	select {
	case res = <-results:
	case <-ctx.Done():
		return nil, errLoginCanceled
	case <-time.After(5 * time.Minute):
		return nil, errors.New("se agotó el tiempo para iniciar sesión; inténtalo de nuevo")
	}
	if res.err != nil {
		return nil, res.err
	}

	ms, err := msTokenRequest(url.Values{
		"client_id":     {clientID},
		"grant_type":    {"authorization_code"},
		"code":          {res.code},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
		"scope":         {msScope},
	})
	if err != nil {
		return nil, fmt.Errorf("Microsoft rechazó el inicio de sesión: %w", err)
	}
	return minecraftFromMicrosoft(ms)
}

// callbackHandler lo fija cada intento de inicio de sesión.
var callbackHandler http.HandlerFunc

func serveCallback(ln net.Listener) {
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { callbackHandler(w, r) }),
		ReadHeaderTimeout: 10 * time.Second,
	}
	srv.Serve(ln)
}

func writeCallbackPage(w http.ResponseWriter, ok bool, msg string) {
	color := "#0d6f68"
	if !ok {
		color = "#a3322b"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Umbral</title>
<body style="font-family:system-ui,sans-serif;background:#0e131a;color:#e6eaee;display:grid;place-items:center;height:100vh;margin:0">
<div style="max-width:420px;padding:28px;border:1px solid #2a3644;border-radius:8px;background:#151c26">
<div style="font-size:22px;font-weight:700;letter-spacing:.14em;text-transform:uppercase;color:%s">Umbral</div>
<p style="font-size:16px;line-height:1.5">%s</p></div></body>`, color, html.EscapeString(msg))
}

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type msToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func msTokenRequest(form url.Values) (*msToken, error) {
	resp, err := httpClient.PostForm(msTokenURL, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var t msToken
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	if t.Error != "" || t.AccessToken == "" {
		return nil, fmt.Errorf("%s %s", t.Error, t.ErrorDesc)
	}
	return &t, nil
}

func postJSON(u string, body any, out any) (int, []byte, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK && out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, raw, err
		}
	}
	return resp.StatusCode, raw, nil
}

type xboxToken struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		XUI []struct {
			UHS string `json:"uhs"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
}

func minecraftFromMicrosoft(ms *msToken) (*Account, error) {
	var xbl xboxToken
	status, _, err := postJSON(xblURL, map[string]any{
		"Properties": map[string]any{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + ms.AccessToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}, &xbl)
	if err != nil {
		return nil, fmt.Errorf("no se pudo contactar con Xbox Live: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Xbox Live rechazó la cuenta (código %d)", status)
	}

	var xsts xboxToken
	status, raw, err := postJSON(xstsURL, map[string]any{
		"Properties": map[string]any{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xbl.Token},
		},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}, &xsts)
	if err != nil {
		return nil, fmt.Errorf("no se pudo contactar con Xbox Live: %w", err)
	}
	if status != http.StatusOK {
		var xe struct {
			XErr int64 `json:"XErr"`
		}
		json.Unmarshal(raw, &xe)
		switch xe.XErr {
		case 2148916233:
			return nil, errors.New("esta cuenta de Microsoft no tiene perfil de Xbox; créalo en xbox.com e inténtalo de nuevo")
		case 2148916235:
			return nil, errors.New("Xbox Live no está disponible en el país de esta cuenta")
		case 2148916236, 2148916237:
			return nil, errors.New("la cuenta necesita verificación de edad en xbox.com")
		case 2148916238:
			return nil, errors.New("es una cuenta de menor: un adulto debe añadirla a un grupo familiar de Microsoft")
		}
		return nil, fmt.Errorf("Xbox Live rechazó la cuenta (XErr %d)", xe.XErr)
	}
	if len(xsts.DisplayClaims.XUI) == 0 {
		return nil, errors.New("respuesta inesperada de Xbox Live")
	}

	var mc struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	status, raw, err = postJSON(mcLoginURL, map[string]string{
		"identityToken": "XBL3.0 x=" + xsts.DisplayClaims.XUI[0].UHS + ";" + xsts.Token,
	}, &mc)
	if err != nil {
		return nil, fmt.Errorf("no se pudo contactar con Minecraft: %w", err)
	}
	if status != http.StatusOK {
		if strings.Contains(string(raw), "Invalid app registration") {
			return nil, errors.New("Mojang aún no ha aprobado la aplicación de Azure de este launcher; mira LEEME.md")
		}
		return nil, fmt.Errorf("Minecraft rechazó el inicio de sesión (código %d)", status)
	}

	name, uuid, err := fetchProfile(mc.AccessToken)
	if err != nil {
		return nil, err
	}
	return &Account{
		RefreshToken: ms.RefreshToken,
		AccessToken:  mc.AccessToken,
		Expires:      time.Now().Add(time.Duration(mc.ExpiresIn) * time.Second),
		UUID:         uuid,
		Name:         name,
	}, nil
}

var errNoGame = errors.New("esta cuenta no tiene Minecraft: Java Edition")

// fetchProfile confirma que la cuenta tiene Minecraft Java.
func fetchProfile(token string) (name, uuid string, err error) {
	req, _ := http.NewRequest(http.MethodGet, mcProfileURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("no se pudo contactar con Minecraft: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", "", errNoGame
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("no se pudo leer el perfil (código %d)", resp.StatusCode)
	}
	var p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return "", "", err
	}
	return p.Name, p.ID, nil
}
