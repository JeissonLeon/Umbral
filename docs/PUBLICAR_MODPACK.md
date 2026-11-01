# Publicar una actualización del modpack

## Qué tocar

En `publicacion/`:

| Cambio | Cómo |
| --- | --- |
| Mod o configuración tuyos | Ponlo, cámbialo o quítalo de `contenido/` (`contenido/mods/…`, `contenido/config/…`) |
| Mod de terceros | Edita `externos` en `publicacion.json` (`path`, `url`, `name`, `description`, `optional`) |
| Nuevo JAR de Umbral | Sustituye `contenido/mods/umbral_sombras-X.Y.Z.jar`; la versión sale del nombre |
| Config que el jugador puede editar | Añade su ruta a `seed`: no se pisa hasta que publiques otra versión de ella |
| Archivo opcional de `contenido/` | Añade su ruta a `opcionales` |
| Novedades | `novedades` en `publicacion.json` |

Lo demás se calcula solo: hashes, tamaños, URLs, qué retirar, secuencia y hash del instalador de Forge. Un archivo retirado de la definición se elimina de los jugadores sin más pasos.

## Publicar

**Con GitHub Actions:** haz commit y push, luego **Actions → Publicar → Run workflow** con la versión.

**En local:**

```bash
go run ./cmd/umbral-publisher release --version 1.2.0 --previous latest --key-file <ruta a umbral-firma.key>
```

```bash
go run ./cmd/umbral-publisher verify --dir dist/release
```

```bash
go run ./cmd/umbral-publisher publish
```

Tras esto, cada launcher lo detecta al abrirse, cada 30 minutos o al pulsar «Buscar y revisar archivos».

## Ensayar sin publicar

`tools/ensayo.sh` genera una versión firmada para servirla en local con `tools/servidor-pruebas`; en `config.json` del launcher de prueba pon `"manifiesto_url": "http://127.0.0.1:8765/manifest.json"`. Solo se acepta HTTP hacia la propia máquina, y la firma se exige igual.

**Cuidado:** la secuencia no puede bajar. Un launcher que aplicó la 5 rechaza la 4 aunque esté firmada. Para deshacer algo, publica una versión nueva con el contenido anterior.
