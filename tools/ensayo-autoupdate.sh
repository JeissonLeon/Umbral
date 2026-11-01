#!/usr/bin/env bash
# Ensayo real del actualizador: 1.0.0 -> 1.1.0 (correcta) -> 1.2.0 (rota, debe revertir).
# Necesita el servidor de pruebas sirviendo <W>/srv en el puerto 8765.
set -uo pipefail
W="$1"; S="$W/sa"
rm -rf "$S"; mkdir -p "$S/app" "$S/v2" "$S/v3" "$S/home"
ld() { go build -trimpath -ldflags "-X main.launcherVersion=$1" -o "$2/UmbralLauncher.exe" .; go build -trimpath -o "$2/umbral-updater.exe" ./cmd/umbral-updater; }
ld 1.0.0 "$S/app"; ld 1.1.0 "$S/v2"
cp "$S/v2/umbral-updater.exe" "$S/v3/"; go build -o "$S/v3/UmbralLauncher.exe" ./cmd/umbral-publisher # sale al instante: versión rota
echo '{"memoria_mb":4096,"mods_extra":[],"manifiesto_url":"http://127.0.0.1:8765/manifest.json","modo_inicio":"externo"}' > "$S/home/config.json"
export UMBRAL_HOME="$S/home" UMBRAL_NO_WINDOW=1

start() { "$S/app/UmbralLauncher.exe" > "$S/lanz.log" 2>&1 & sleep 4; URL=$(grep -a Interfaz "$S/lanz.log" | sed 's/.*en //'); BASE=${URL%/\?*}; TOK=${URL##*t=}; }
ver() { curl -s -H "X-Umbral-Token: $TOK" "$BASE/api/estado" | grep -o '"version_launcher":"[^"]*"\|"launcher_nuevo":"[^"]*"'; }
kill_all() { powershell -NoProfile -Command "Get-Process UmbralLauncher -EA 0 | Where-Object { \$_.Path -like '*ensayo*' } | Stop-Process -Force"; }

echo "== 1.1.0 correcta"; bash tools/ensayo.sh "$W" 1.2.0 yes "$S/v2" 1.1.0 | tail -1
start; sleep 3; ver
curl -s -X POST -H "X-Umbral-Token: $TOK" "$BASE/api/launcher/actualizar"; echo
sleep 8
sha256sum "$S/app/UmbralLauncher.exe" "$S/v2/UmbralLauncher.exe" | cut -c1-16; ls "$S/app"; cat "$S/home/updater.log"
kill_all; sleep 1

echo "== 1.2.0 rota"; bash tools/ensayo.sh "$W" 1.3.0 yes "$S/v3" 1.2.0 | tail -1
start; sleep 3; ver
curl -s -X POST -H "X-Umbral-Token: $TOK" "$BASE/api/launcher/actualizar"; echo
sleep 10
sha256sum "$S/app/UmbralLauncher.exe" "$S/v2/UmbralLauncher.exe" | cut -c1-16; ls "$S/app"; tail -3 "$S/home/updater.log"; cat "$S/home/launcher-rechazado.json"; echo
powershell -NoProfile -Command "(Get-Process UmbralLauncher -EA 0 | Where-Object { \$_.Path -like '*ensayo*' }).Count"
kill_all; sleep 1; start; echo "tras reabrir, no debe ofrecer 1.2.0:"; ver; kill_all
