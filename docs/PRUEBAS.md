# Pruebas

Resultado del 1 de noviembre de 2026, en Windows 11 Pro con Go 1.27.3: `go test ./...` pasa, 62 pruebas y ninguna omitida.

```bash
go test ./...
```

## Las 16 pruebas obligatorias

| # | Prueba | Automática | Ensayo real |
| --- | --- | --- | --- |
| 1 | Instalación desde cero | `TestInstallFromScratch` | Sí: Java, Minecraft, assets, Forge y 3 mods, servidos en local |
| 2 | Descarga de un archivo | `TestInstallFromScratch`, `TestIncrementalDownloadsOnlyChanges` | Sí |
| 3 | Verificación SHA-256 | `TestCorruptDownloadRejected` | Sí |
| 4 | Archivo corrupto | `TestCorruptDownloadRejected`, `TestRestoresModifiedManagedFile` | Sí: un mod alterado a mano se reemplazó |
| 5 | Archivo ausente | `TestMissingFile`, `TestOptionalFailureDoesNotBlock` | No |
| 6 | Actualización de un mod | `TestReconcileAddUpdateRemove` | Sí |
| 7 | Mod nuevo | `TestReconcileAddUpdateRemove` | Sí, con un archivo de configuración nuevo |
| 8 | Eliminación de un mod administrado | `TestReconcileAddUpdateRemove`, `TestRemoveListRespectsOwnership` | Sí |
| 9 | Actualización de configuración | `TestSeedConfigKeepsPlayerEditsUntilManifestChanges` | Sí: la configuración personal se conservó |
| 10 | Actualización del launcher | `internal/selfupdate` | Sí, ver abajo |
| 11 | Interrupción de una descarga | `TestInterruptedDownloadResumes`, `TestRecoverRollsBackInterruptedTransaction` | No |
| 12 | Pérdida de conexión | `TestConnectionLoss`, `TestRefreshOfflineUsesCache` | No |
| 13 | Manifiesto inválido | `TestInvalidManifests`, `TestSafePath` | No |
| 14 | Firma incorrecta | `TestSignature`, `TestFetchManifestRejectsBadSignatureAndKeepsCache`, `TestRejectsBadSignature` | No |
| 15 | Directorio sin permisos | `TestUnwritableInstallDir`, `TestCheckWritable` | No. Se simula con una ruta bloqueada, no con permisos NTFS denegados |
| 16 | Inicio de Minecraft | `TestArranqueReal` (etiqueta `juego`, fuera de `go test ./...`) | Sí, ver abajo |

## Ensayos reales

**Modpack.** `tools/ensayo.sh` firma una publicación con la clave real y `tools/servidor-pruebas` la sirve en `127.0.0.1`. Son descargas reales de Mojang y Forge, pero el modpack sale de ese servidor local, no de GitHub.

**Juego.** `TestArranqueReal` abrió Minecraft 1.20.1 con Forge 47.4.10 y el mod Umbral desde la instancia instalada, hasta cargar los mods, los atlas de texturas y el sonido. Usó una cuenta ficticia que solo existe en esa prueba. No se entró en un mundo.

```bash
UMBRAL_HOME=<carpeta instalada> go test -tags juego -run TestArranqueReal -timeout 10m .
```

La prueba deja el juego abierto: hay que cerrarlo a mano.

**Actualizador.** `tools/ensayo-autoupdate.sh` usa ejecutables reales:

- De 1.0.0 a 1.1.0: el ejecutable instalado quedó idéntico al publicado.
- De 1.1.0 a 1.2.0, que se cierra al arrancar: se restauró 1.1.0, volvió a abrirse y, al reabrir el launcher, ya no ofrecía 1.2.0.

**Instalador.** `UmbralSetup.exe` en modo silencioso, en una carpeta elegida: creó los archivos, los accesos directos y el registro de desinstalación, y el launcher abrió su ventana WebView2 real. La desinstalación silenciosa quitó programa, accesos y registro, y conservó los datos.

**GitHub.** Los flujos `Pruebas` y `Publicar` terminaron bien y publicaron `sylvanor3-1.0.0-1` en JeissonLeon/Umbral. Con el `UmbralSetup.exe` descargado de esa versión se instaló el launcher, luego el modpack desde GitHub con sus 8 mods, y `TestArranqueReal` abrió el juego desde esa instancia. El modo de inicio oficial y el botón Jugar no intervinieron: la prueba arranca el juego directamente.

## Sin probar

- Una actualización del launcher publicada en GitHub: solo se ha hecho desde el servidor local. La del modpack sí se comprobó, de 1.0.0 a 1.1.0, con Epic Fight 20.14.17 y Weapons of Miracles 2.0.171. El juego siguió abierto 30 segundos tras cargar; el registro muestra un error de WoM al leer la animación `valkyrie_idle`.
- El combate de Epic Fight y WoM con las clases de Umbral 0.3.0 dentro de un mundo.
- El modo oficial contra el launcher real de Minecraft. Solo tiene pruebas sobre carpetas temporales, para no tocar el `.minecraft` del equipo.
- El inicio de sesión de Microsoft con una cuenta real: requiere un `CLIENT_ID` aprobado.
- El asistente gráfico del instalador, página por página.
- Un Windows limpio, sin WebView2 ni Java. Este equipo ya tiene WebView2.
- Entrar en un mundo y jugar en multijugador.
