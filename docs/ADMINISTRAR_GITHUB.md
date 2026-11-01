# Administrar GitHub

GitHub es la única infraestructura. El launcher lee un manifiesto en una dirección fija, sin API ni token:

`https://github.com/<owner>/<repo>/releases/latest/download/manifest.json` (y `.sig`)

**El repositorio debe ser público**, o los jugadores no podrán descargar. Si prefieres no publicar el código, usa un repositorio público aparte solo para las versiones.

## Puesta en marcha

1. Crea el repositorio y sube este proyecto.
2. Rellena `github.owner` y `github.repo` en `publicacion/publicacion.json`.
3. Prepara la clave de firma:
   - Ya existe una en `%USERPROFILE%\.umbral\firma\umbral-firma.key`, y su parte pública está en `internal/pack/trusted_keys.txt`. **Haz una copia en un lugar seguro:** sin ella no podrás publicar para los launchers instalados.
   - Para otra clave: `go run ./cmd/umbral-publisher keygen --out <carpeta>` y sustituye la línea en `trusted_keys.txt`, antes de repartir el primer instalador.
4. En GitHub: **Settings → Secrets and variables → Actions → New repository secret**, nombre `UMBRAL_SIGNING_KEY`, valor el contenido del archivo `.key`.
5. Primera publicación: **Actions → Publicar → Run workflow**, con versión, versión del launcher y `primera` marcado. También se puede hacer en local ([PUBLICAR_MODPACK.md](PUBLICAR_MODPACK.md)).

## Permisos

| Quién | Necesita |
| --- | --- |
| GitHub Actions | `contents: write`, ya declarado en el workflow; usa `GITHUB_TOKEN` |
| Publicar en local | [GitHub CLI](https://cli.github.com) con `gh auth login`, o `GH_TOKEN` con permiso *Contents: read and write* |
| Jugadores | Nada |

Nunca pongas tokens ni la clave privada en el código, el manifiesto, los ejecutables o el repositorio.

## Estructura de una versión

Etiqueta `sylvanor3-<versión>-<secuencia>`, con `manifest.json`, `manifest.json.sig`, los archivos nuevos (nombre `<hash>-<archivo>`) y `UmbralSetup.exe` si cambió el launcher.

- **No borres versiones antiguas:** un archivo que no cambia se reutiliza de la versión donde se subió, y su URL sigue en los manifiestos nuevos.
- Se crea como borrador y solo pasa a "última" tras subir todo: un launcher no ve versiones a medias.
- Límites de GitHub: 2 GB por archivo y 1000 por versión.
- Los mods de terceros no se vuelven a alojar: `externos` apunta a su sitio original. Si subes un mod a `contenido/`, comprueba que su licencia lo permita.

## Mods grandes en el repositorio

`publicacion/contenido/` entra en Git porque Actions lo lee de ahí. Para archivos grandes usa Git LFS (el workflow ya lo descarga), o publica en local y deja `contenido/` fuera con `.gitignore`.
