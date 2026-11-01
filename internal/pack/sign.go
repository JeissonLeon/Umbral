package pack

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrBadSignature indica que un manifiesto no viene de quien publica el modpack.
var ErrBadSignature = errors.New("la firma del manifiesto no es válida")

// trustedKeysText contiene una clave pública en base64 por línea. Para rotar,
// se añade la nueva, se publica un launcher y solo después se firma con ella
// (docs/SEGURIDAD.md).
//
//go:embed trusted_keys.txt
var trustedKeysText string

// Signature es el contenido de manifest.json.sig.
type Signature struct {
	KeyID string `json:"keyId"`
	Sig   string `json:"sig"`
}

// KeyID es la identidad corta de una clave pública.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// TrustedKeys devuelve las claves incluidas en este ejecutable.
func TrustedKeys() ([]ed25519.PublicKey, error) { return ParseKeys(trustedKeysText) }

// ParseKeys lee una clave por línea; ignora vacías y comentarios (#).
func ParseKeys(text string) ([]ed25519.PublicKey, error) {
	var keys []ed25519.PublicKey
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(line)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("clave pública no válida: %q", line)
		}
		keys = append(keys, ed25519.PublicKey(raw))
	}
	return keys, nil
}

// Sign firma los bytes exactos del manifiesto.
func Sign(manifest []byte, priv ed25519.PrivateKey) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("clave privada no válida")
	}
	return json.MarshalIndent(Signature{
		KeyID: KeyID(priv.Public().(ed25519.PublicKey)),
		Sig:   base64.StdEncoding.EncodeToString(ed25519.Sign(priv, manifest)),
	}, "", "  ")
}

// Verify comprueba la firma contra las claves de confianza.
func Verify(manifest, sig []byte, keys []ed25519.PublicKey) error {
	if len(keys) == 0 {
		return fmt.Errorf("%w: este launcher se compiló sin claves públicas", ErrBadSignature)
	}
	var s Signature
	if err := json.Unmarshal(sig, &s); err != nil {
		return fmt.Errorf("%w: archivo de firma dañado", ErrBadSignature)
	}
	raw, err := base64.StdEncoding.DecodeString(s.Sig)
	if err != nil {
		return fmt.Errorf("%w: archivo de firma dañado", ErrBadSignature)
	}
	for _, k := range keys {
		if KeyID(k) == s.KeyID && ed25519.Verify(k, manifest, raw) {
			return nil
		}
	}
	return ErrBadSignature
}

// LoadVerified verifica y luego interpreta: nada sin firma llega al parser.
func LoadVerified(manifest, sig []byte, keys []ed25519.PublicKey) (*Manifest, error) {
	if err := Verify(manifest, sig, keys); err != nil {
		return nil, err
	}
	return Parse(manifest)
}

// GenerateKey crea un par de claves; ambas se devuelven en base64.
func GenerateKey() (pub, priv string, err error) {
	p, s, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(p), base64.StdEncoding.EncodeToString(s), nil
}

// ParsePrivateKey lee una clave privada en base64.
func ParsePrivateKey(text string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("clave privada no válida")
	}
	return ed25519.PrivateKey(raw), nil
}
