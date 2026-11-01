# Umbral Launcher

Launcher de **SylvanΩr 3** (Minecraft 1.20.1, Forge 47.4.10, Java 17) para Windows. El jugador instala `UmbralSetup.exe`; el launcher descarga el juego y el modpack, lo mantiene igual que la versión publicada en GitHub y se actualiza a sí mismo. No hay servidor propio, base de datos ni cuentas de Umbral: GitHub Releases es toda la infraestructura.

## Qué hace

| Parte | Detalle |
| --- | --- |
| Instalador | NSIS. Por usuario, sin administrador, con carpeta a elegir, accesos directos y entrada en "Agregar o quitar programas". Comprueba Windows 10+ de 64 bits y WebView2. |
| Instalación del juego | Java oficial de Mojang (o el tuyo), Minecraft, recursos y Forge con su instalador oficial, en una instancia aislada (`%APPDATA%\umbral`). |
| Reconciliación | Compara cada archivo por SHA-256 con el manifiesto firmado: añade, reemplaza, restaura y elimina solo lo necesario. Lo que no registró nunca lo borra. |
| Recuperación | Descargas reanudables, verificación antes de tocar nada, transacción con respaldo y marcha atrás si algo falla o se corta. |
| Autoactualización | `umbral-updater.exe` reemplaza el launcher, comprueba que la versión nueva arranque y, si no, restaura la anterior. |
| Publicación | `umbral-publisher` genera y firma el manifiesto y sube a GitHub Releases; también desde GitHub Actions. |

## Cómo se abre el juego

Umbral prepara la instancia; la cuenta de Minecraft es de otro. Se elige en Ajustes:

| Modo | Qué hace | Credenciales |
| --- | --- | --- |
| **Launcher oficial** (por defecto) | Añade Forge y un perfil «SylvanΩr 3» al launcher oficial y lo abre. Ahí se pulsa Jugar. | Las gestiona Minecraft |
| **Otro launcher** | Solo instala y actualiza; indica la carpeta para Prism, ATLauncher, etc. | Las gestiona ese launcher |
| **Directo con Microsoft** | Abre el juego desde Umbral. Solo aparece si compilas con `CLIENT_ID` (requiere registrar la app en Azure y la aprobación de Mojang). | Solo en memoria; nunca en disco |

No hay modo sin cuenta: hace falta una copia original de Minecraft: Java Edition.

## Compilar

Requiere Go 1.24+ y NSIS (`makensis`).

```bash
VERSION=1.0.0 MANIFEST_URL=https://github.com/<owner>/<repo>/releases/latest/download/manifest.json ./build.sh
```

Genera en `dist/`: `UmbralSetup.exe` (lo que se reparte), `UmbralLauncher.exe`, `umbral-updater.exe` y `umbral-publisher.exe`.

```bash
go test ./...
```

## Documentación

| Documento | Para qué |
| --- | --- |
| [docs/INSTALACION.md](docs/INSTALACION.md) | Instalar y resolver problemas, para jugadores |
| [docs/ADMINISTRAR_GITHUB.md](docs/ADMINISTRAR_GITHUB.md) | Preparar el repositorio, la clave y los permisos |
| [docs/PUBLICAR_MODPACK.md](docs/PUBLICAR_MODPACK.md) | Publicar una actualización del modpack |
| [docs/PUBLICAR_LAUNCHER.md](docs/PUBLICAR_LAUNCHER.md) | Publicar una actualización del launcher |
| [docs/MANIFIESTO.md](docs/MANIFIESTO.md) | Formato del manifiesto y reglas de propiedad de archivos |
| [docs/SEGURIDAD.md](docs/SEGURIDAD.md) | Firmas, rotación de claves, ofuscación y multijugador |
| [docs/PRUEBAS.md](docs/PRUEBAS.md) | Qué se probó y cómo |
| [config.ejemplo.json](config.ejemplo.json) | Ajustes del jugador |

## Código

| Ruta | Contenido |
| --- | --- |
| `main.go`, `app.go`, `server.go`, `events.go` | Arranque, estado, API local y eventos para la interfaz |
| `manifest.go` | Descarga y verificación del manifiesto, copia local, anti-retroceso |
| `mojang.go`, `java.go`, `forge.go`, `mods.go` | Preparación de Minecraft |
| `launchers.go`, `launch.go`, `auth.go` | Modos de arranque |
| `config.go`, `ping.go` | Ajustes y estado del servidor |
| `window_*.go`, `ui/` | Ventana WebView2 e interfaz |
| `internal/pack` | Esquema, firma, descargas y reconciliación |
| `internal/selfupdate` | Lógica de autoactualización |
| `cmd/umbral-updater`, `cmd/umbral-publisher` | Ejecutables auxiliares |
| `publicacion/` | Definición del modpack que se publica |
| `installer/umbral.nsi` | Instalador |
| `tools/` | Ensayo de publicaciones sin GitHub |

Para desarrollar: `UMBRAL_HOME=<carpeta> UMBRAL_NO_WINDOW=1 go run .` sirve la interfaz para abrirla en un navegador.
