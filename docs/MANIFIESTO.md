# Manifiesto (esquema 1)

`manifest.json` es la definición completa del modpack y la fuente de verdad: si choca con lo instalado, gana el manifiesto. `manifest.json.sig` firma sus bytes exactos. Lo genera `umbral-publisher`; no se edita a mano. Lo valida `internal/pack/manifest.go`.

```json
{
  "schemaVersion": 1,
  "sequence": 7,
  "publishedAt": "2026-11-01T18:00:00Z",
  "modpack": { "id": "sylvanor3", "name": "SylvanΩr 3", "version": "1.2.0", "minecraft": "1.20.1", "forge": "47.4.10", "java": 17, "forgeInstallerSha256": "…" },
  "launcher": { "version": "1.1.0", "files": [ { "name": "UmbralLauncher.exe", "url": "https://…", "sha256": "…", "size": 1 } ] },
  "umbralMod": { "version": "0.3.0", "path": "mods/umbral_sombras-0.3.0.jar" },
  "news": ["…"],
  "files": [
    { "path": "mods/umbral_sombras-0.3.0.jar", "version": "ab12cd34ef56", "url": "https://…", "sha256": "…", "size": 919224, "required": true, "policy": "managed", "name": "", "description": "" }
  ],
  "remove": [ { "path": "mods/viejo.jar", "sha256": "…" } ]
}
```

| Campo | Regla |
| --- | --- |
| `sequence` | Crece en cada publicación. Un launcher rechaza una menor que la ya vista |
| `path` | Relativa, con `/`, dentro de `mods`, `config`, `defaultconfigs`, `resourcepacks`, `shaderpacks`, `kubejs`, `scripts` o `patchouli_books`. Nunca `saves`, `..`, unidades ni nombres reservados de Windows |
| `url` | HTTPS |
| `required` | `false` = opcional; el jugador lo activa en Mods. Si falla su descarga no bloquea |
| `policy` | `managed` (por defecto): se restaura si difiere. `seed`: respeta las ediciones del jugador hasta que el manifiesto traiga otro hash |

Un launcher rechaza un `schemaVersion` que no conoce: un cambio incompatible exige publicar antes el launcher que lo entienda.

## Propiedad de los archivos

El launcher guarda en `estado/instalacion.json` qué instaló.

| Categoría | Ejemplos | Trato |
| --- | --- | --- |
| A. Administrados | Lo registrado o en `files` | Se reemplazan, restauran y eliminan |
| B. Generados | `versions`, `libraries`, `assets`, `runtime`; `logs`, `crash-reports` | Los gestionan Mojang, Forge y el juego; no están en el registro |
| C. Del jugador | `saves`, `screenshots`, `options.txt`, config propia | Nunca se borran. Excepción: un `.jar` ajeno en `mods` se mueve a `mods-desactivados` |

Un archivo de `remove` solo se borra si está registrado o si su hash coincide. Nunca se borra una carpeta completa.

## Orden al aplicar

1. Descarga todo a `estado/descargas` (reanudable) y verifica tamaño y SHA-256.
2. Escribe la transacción con el respaldo de cada archivo.
3. Mueve lo viejo a `estado/respaldo` y lo nuevo a su sitio.
4. Escribe el registro: **este es el punto de confirmación**. Hasta aquí no consta como actualizada.
5. Limpia.

Si falla el paso 3 o 4, se deshace en el momento. Si se corta la luz, se deshace al arrancar.
