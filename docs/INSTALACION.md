# Instalar SylvanΩr 3

1. Descarga `UmbralSetup.exe` de la página de versiones del proyecto en GitHub.
2. Ábrelo. Si Windows muestra "Windows protegió su PC": **Más información → Ejecutar de todas formas** (el instalador no lleva certificado de firma de código).
3. Elige carpeta, termina y abre Umbral.
4. Pulsa **Instalar**. Se descargan cerca de 1 GB. Si se corta, vuelve a pulsar y continúa donde iba.
5. Pulsa **Jugar**. Se abre el launcher oficial de Minecraft: elige el perfil **SylvanΩr 3** y pulsa Jugar allí.

Requisitos: Windows 10 u 11 de 64 bits y una copia original de Minecraft: Java Edition. Java 17 lo descarga Umbral desde Mojang. WebView2 suele venir con Windows; si falta, el instalador lo ofrece.

## Carpetas

| Carpeta | Contenido |
| --- | --- |
| `%LOCALAPPDATA%\Programs\Umbral` | El launcher |
| `%APPDATA%\umbral` | Juego, Java, Forge; se puede cambiar en Ajustes |
| `%APPDATA%\umbral\instancia` | La instancia: mods, config, mundos, capturas |

## Qué conserva una actualización

- Tus mundos, capturas y opciones: un manifiesto no puede tocar `saves` ni nada fuera de `mods`, `config` y similares.
- Tus archivos propios en `config`: solo se reemplaza lo que instaló Umbral.
- Un `.jar` que añadas a `mods` se mueve a `mods-desactivados`, sin borrarse.

## Problemas

| Mensaje | Qué hacer |
| --- | --- |
| Sin permiso de escritura | Cierra Minecraft, o elige otra carpeta en Ajustes |
| No encontré el launcher oficial | Instálalo y ábrelo una vez, o corrige su carpeta en Ajustes |
| No aparece el perfil | Cierra el launcher oficial y pulsa Jugar en Umbral otra vez |
| La firma no es válida | No instales nada; avisa a quien publica el modpack |
| Es Java N; necesita Java 17 | Vacía la ruta de Java en Ajustes |

Para pedir ayuda: **Ajustes → Ver diagnóstico → Copiar**, más `launcher.log` (botón «Registros del launcher»). No contienen credenciales.
