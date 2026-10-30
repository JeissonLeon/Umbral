# Umbral Launcher

Launcher del modpack Umbral para Windows. El jugador instala `UmbralSetup.exe`, inicia sesión con la cuenta de Microsoft con la que compró Minecraft: Java Edition, y el launcher descarga e instala todo lo demás: Minecraft 1.20.1, Java 17, Forge 47.4.10, el paquete de optimización y los mods extra que active.

## Qué hace

| Parte | Detalle |
| --- | --- |
| Instalador | Instala en `%LOCALAPPDATA%\Programs\Umbral` sin pedir administrador, crea accesos directos en escritorio y menú Inicio, y aparece en "Agregar o quitar programas". Al desinstalar pregunta si borrar también los datos del juego. |
| Inicio de sesión | Flujo oficial de Microsoft en el navegador del sistema (OAuth con PKCE). El launcher nunca ve la contraseña. Comprueba que la cuenta tenga Minecraft Java y renueva la sesión sola. |
| Instalación del juego | Java oficial de Mojang, Minecraft, recursos, Forge con su instalador oficial y mods de Modrinth. Todo se verifica por huella SHA-1 y solo se descarga lo que falta. |
| Mods | Modpack base fijo y mods extra de una lista permitida, que el jugador activa desde la pestaña Mods. Cualquier otro `.jar` se aparta a `mods-desactivados`. |
| Servidor | Muestra estado, jugadores y latencia del servidor configurado. Al jugar entra directo a él. |
| Actualizaciones | El modpack se puede actualizar publicando un manifiesto remoto, sin reinstalar el launcher. |

Datos del jugador: `%APPDATA%\umbral` (juego en `instancia`, registro del launcher en `launcher.log`, registro del juego en `instancia\logs\umbral-juego.log`).

## Antes de distribuirlo: registrar la aplicación de Microsoft

Sin este paso el launcher se abre pero no puede iniciar sesión.

1. Entra en [portal.azure.com](https://portal.azure.com) → **Microsoft Entra ID** → **Registros de aplicaciones** → **Nuevo registro**.
2. Nombre: `Umbral Launcher`. Tipos de cuenta: **Solo cuentas personales de Microsoft**.
3. URI de redirección: plataforma **Cliente público/nativo (móvil y escritorio)**, valor `http://localhost`.
4. Pulsa **Registrar** y copia el **Id. de aplicación (cliente)**.
5. Mojang tiene que aprobar las aplicaciones nuevas antes de que puedan usar el inicio de sesión de Minecraft. Envía tu ID de aplicación en su formulario de revisión (aka.ms/mce-reviewappid). Mientras no lo aprueben, el launcher muestra "Mojang aún no ha aprobado la aplicación de Azure de este launcher".
6. Compila con ese ID (siguiente sección).

## Compilar

Requiere Go 1.24+ y NSIS (`makensis`). En Linux: `sudo apt install nsis`.

```
CLIENT_ID=<id de aplicación> ./build.sh
```

Genera `dist/UmbralLauncher.exe` y `dist/UmbralSetup.exe`. Lo que se reparte es `UmbralSetup.exe`.

Variables opcionales:

| Variable | Uso |
| --- | --- |
| `MANIFEST_URL` | URL de un `manifiesto.json` publicado (por ejemplo en GitHub). El launcher lo consulta al pulsar Jugar y guarda copia para jugar sin conexión. |
| `VERSION` | Versión del launcher y del instalador (por defecto 0.2.0). |

## Actualizar el modpack

Edita `manifiesto.json`: versiones de Minecraft y Forge, mods base (`mods`), mods extra permitidos (`extras`) y novedades. Cada mod necesita nombre de archivo, URL y SHA-1. Si compilaste con `MANIFEST_URL`, basta con publicar el archivo nuevo en esa URL; si no, recompila.

## Avisos para los jugadores

- **SmartScreen:** al no estar firmado, Windows puede mostrar "Windows protegió su PC". Se abre con **Más información → Ejecutar de todas formas**. Un certificado de firma de código lo evita.
- **WebView2:** la ventana usa Microsoft Edge WebView2, incluido en Windows 10 y 11. Si falta, el instalador y el launcher ofrecen descargarlo.
- **Ajustes:** servidor y memoria se cambian en la pestaña Ajustes.

## Desarrollo

Fuera de Windows la interfaz se abre en el navegador, lo que permite probarla:

```
UMBRAL_HOME=/tmp/umbral go run .
go test ./...
```

| Archivo | Contenido |
| --- | --- |
| `main.go`, `app.go`, `server.go`, `events.go` | Arranque, estado, API local y eventos para la interfaz |
| `auth.go` | Inicio de sesión de Microsoft, Xbox Live y Minecraft |
| `mojang.go`, `java.go`, `forge.go`, `mods.go`, `launch.go` | Instalación y arranque del juego |
| `ping.go` | Estado del servidor de Minecraft |
| `window_windows.go`, `window_other.go` | Ventana WebView2 y utilidades por sistema |
| `ui/` | Interfaz (HTML, CSS, JS) |
| `installer/umbral.nsi` | Instalador |
