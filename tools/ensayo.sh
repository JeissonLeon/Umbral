#!/usr/bin/env bash
# Ensayo de una publicación sin GitHub: firma una versión y la deja lista para
# servir en local. Uso: tools/ensayo.sh <carpeta-de-trabajo> <version> <secuencia-anterior|none> [launcher-dir launcher-version]
set -euo pipefail
W="$1"; V="$2"; PREV="$3"; LD="${4:-}"; LV="${5:-1.0.0}"
KEY="${UMBRAL_KEY_FILE:-$USERPROFILE/.umbral/firma/umbral-firma.key}"
PREVARG=(--previous none); [[ "$PREV" != none ]] && PREVARG=(--previous "$W/srv/manifest.json")
[[ "$PREV" != none ]] && cp "$W/srv/manifest.json" "$W/prev.json" && PREVARG=(--previous "$W/prev.json")
LARG=(--launcher-version "$LV"); [[ -n "$LD" ]] && LARG+=(--launcher-dir "$LD")
go run ./cmd/umbral-publisher release --config "$W/pub.json" --version "$V" "${LARG[@]}" "${PREVARG[@]}" \
  --key-file "$KEY" --out "$W/rel" --base-url http://127.0.0.1:8765
SEQ=$(grep -m1 '"sequence"' "$W/rel/manifest.json" | tr -dc 0-9)
mkdir -p "$W/srv/sylvanor3-$V-$SEQ"
cp "$W/rel"/assets/* "$W/srv/sylvanor3-$V-$SEQ/" 2>/dev/null || true
cp "$W/rel/manifest.json" "$W/rel/manifest.json.sig" "$W/srv/"
