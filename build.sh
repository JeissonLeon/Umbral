#!/usr/bin/env bash
# Compila el launcher, el actualizador, el publicador y el instalador en dist/.
#
#   VERSION=1.0.0 MANIFEST_URL=https://github.com/<owner>/<repo>/releases/latest/download/manifest.json ./build.sh
#
# CLIENT_ID es opcional: activa el inicio directo con Microsoft (mira LEEME.md).
# Requiere Go 1.24+ y NSIS (makensis). Funciona en Linux, macOS o Windows (Git Bash).
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${VERSION:-1.0.0}"
CLIENT_ID="${CLIENT_ID:-}"
MANIFEST_URL="${MANIFEST_URL:-}"
if [[ -z "$MANIFEST_URL" ]]; then
  echo "Aviso: MANIFEST_URL vacío. El launcher no sabrá dónde buscar el modpack." >&2
fi
if ! grep -qv '^#' internal/pack/trusted_keys.txt; then
  echo "Error: internal/pack/trusted_keys.txt no tiene ninguna clave pública." >&2
  exit 1
fi

mkdir -p dist

# Icono, manifiesto de Windows (DPI, estilos) e información de versión.
go run github.com/tc-hib/go-winres@v0.3.3 simply \
  --arch amd64 \
  --manifest gui \
  --icon assets/icon.png \
  --product-name "Umbral" \
  --file-description "Umbral Launcher" \
  --product-version "$VERSION" \
  --file-version "$VERSION" \
  --original-filename UmbralLauncher.exe

export GOOS=windows GOARCH=amd64
go build -trimpath \
  -ldflags "-s -w -H=windowsgui -X main.defaultClientID=$CLIENT_ID -X main.defaultManifestURL=$MANIFEST_URL -X main.launcherVersion=$VERSION" \
  -o dist/UmbralLauncher.exe .
go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist/umbral-updater.exe ./cmd/umbral-updater
go build -trimpath -ldflags "-s -w" -o dist/umbral-publisher.exe ./cmd/umbral-publisher
unset GOOS GOARCH

makensis -V2 -DVERSION="$VERSION" installer/umbral.nsi

echo "Listo:"
ls -lh dist/UmbralLauncher.exe dist/umbral-updater.exe dist/umbral-publisher.exe dist/UmbralSetup.exe
