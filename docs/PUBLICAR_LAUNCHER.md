# Publicar una actualización del launcher

**Con GitHub Actions:** **Actions → Publicar → Run workflow**, con la versión del modpack y `launcher_version` (por ejemplo `1.1.0`). Compila, genera el instalador, firma y publica.

**En local:**

```bash
VERSION=1.1.0 MANIFEST_URL=https://github.com/<owner>/<repo>/releases/latest/download/manifest.json ./build.sh
```

```bash
mkdir -p dist/lanzar && cp dist/UmbralLauncher.exe dist/umbral-updater.exe dist/lanzar/
```

```bash
go run ./cmd/umbral-publisher release --version <modpack> --previous latest --key-file <clave> --launcher-dir dist/lanzar --launcher-version 1.1.0 --setup dist/UmbralSetup.exe
```

```bash
go run ./cmd/umbral-publisher publish
```

La versión debe ser mayor que la anterior, o el launcher no la ofrece.

## Qué ocurre en cada equipo

1. El launcher avisa y, al aceptar, descarga los ejecutables con verificación de hash.
2. Reemplaza `umbral-updater.exe` si cambió, lanza el actualizador y se cierra.
3. El actualizador verifica **por su cuenta** la firma del manifiesto y el hash, respalda el ejecutable y lo reemplaza de forma atómica.
4. Abre la versión nueva, que debe confirmar en 60 segundos que su ventana está lista.
5. Sin confirmación, restaura la anterior, la vuelve a abrir y no ofrece más esa versión. Se ofrecerá la siguiente que publiques.

Registro: `updater.log` en la carpeta de datos. Si todo falla, `UmbralSetup.exe` reinstala encima sin perder datos.
