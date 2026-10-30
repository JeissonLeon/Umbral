#!/usr/bin/env bash
# Compila UmbralLauncher.exe y el instalador UmbralSetup.exe en dist/.
#
#   CLIENT_ID=<id de aplicación de Azure> [MANIFEST_URL=<url del manifiesto>] ./build.sh
#
# Requiere Go 1.24+ y NSIS (makensis). Funciona en Linux, macOS o Windows (Git Bash).
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${VERSION:-0.2.0}"
CLIENT_ID="${CLIENT_ID:-}"
MANIFEST_URL="${MANIFEST_URL:-}"
if [[ -z "$CLIENT_ID" ]]; then
  echo "Aviso: CLIENT_ID vacío. El launcher se compilará sin poder iniciar sesión." >&2
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

GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -H=windowsgui -X main.defaultClientID=$CLIENT_ID -X main.defaultManifestURL=$MANIFEST_URL -X main.launcherVersion=$VERSION" \
  -o dist/UmbralLauncher.exe .

makensis -V2 -DVERSION="$VERSION" installer/umbral.nsi

echo "Listo:"
ls -lh dist/UmbralLauncher.exe dist/UmbralSetup.exe
