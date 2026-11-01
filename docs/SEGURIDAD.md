# Seguridad

**Lo que no se puede evitar:** cualquier archivo que llega al equipo del jugador se puede copiar. Nada de esto impide copiar un JAR. Sí garantiza que el launcher oficial solo instala lo que tú firmaste.

## Integridad y autenticidad

| Amenaza | Defensa |
| --- | --- |
| Archivo dañado o cambiado en la descarga | SHA-256 y tamaño antes de instalar; HTTPS obligatorio, también en redirecciones |
| Alguien sustituye el manifiesto o un archivo en GitHub | Firma Ed25519 (`crypto/ed25519` de la biblioteca estándar de Go). Un hash solo prueba que el archivo es el del manifiesto; la firma prueba quién publicó el manifiesto |
| Servir una versión antigua bien firmada | `sequence` no puede bajar |
| Alterar la copia local del manifiesto | Se vuelve a verificar la firma al leerla |
| Manifiesto que escribe fuera de la instancia | Validación de rutas y lista de carpetas permitidas |
| Launcher falso entregado por actualización | El actualizador verifica firma y hash por su cuenta, y rechaza bajar de versión |

La firma de los ejecutables es indirecta: su hash va dentro del manifiesto firmado.

**Sin cubrir:** `UmbralSetup.exe` no lleva certificado de firma de código, por lo que SmartScreen avisa. Un primer instalador manipulado no se puede detectar desde dentro; distribúyelo solo desde tu página de GitHub. Requiere comprar un certificado.

## Clave privada

- Solo existe en tu equipo y en el secreto `UMBRAL_SIGNING_KEY`. El launcher lleva únicamente las públicas (`internal/pack/trusted_keys.txt`).
- Quien la tenga puede publicar para todos tus jugadores. Copia cifrada fuera del equipo, y nunca en el repositorio.

### Rotación planificada

1. `umbral-publisher keygen --out <carpeta>`.
2. Añade la pública nueva a `trusted_keys.txt`, **sin quitar la vieja**.
3. Publica ese launcher firmando con la clave vieja y espera a que se actualicen.
4. Cambia el secreto y firma con la nueva.
5. En una versión posterior, quita la vieja.

### Si se filtra

Añade una clave nueva y publica cuanto antes un launcher sin la filtrada. Quien no se actualice sigue aceptando la clave filtrada hasta entonces, y si la perdiste del todo, los launchers instalados solo se recuperan reinstalando con un `UmbralSetup.exe` nuevo. Avisa a los jugadores.

## Credenciales

Umbral no tiene cuentas, no pide contraseñas y no lee `launcher_accounts.json` ni datos de otros launchers. En el modo directo la sesión de Microsoft solo vive en memoria; al actualizar desde 0.2.0 se borra el antiguo `auth.json`. La API local solo escucha en 127.0.0.1, exige un token por sesión y comprueba el `Host`.

Sin comprobaciones remotas de licencia: se puede jugar sin conexión con la última versión verificada.

## Ofuscación del mod: evaluación

**Estado: no aplicada.** No hay ofuscación en el JAR publicado. Reduce la legibilidad y no impide copiar; mal hecha rompe el mod en el arranque de los jugadores, y hay que probarla en el juego real.

Si se aplica (ProGuard sobre el JAR ya reobfuscado a SRG), esto debe quedar intacto:

| No renombrar | Por qué |
| --- | --- |
| Clase `@Mod` y `@Mod.EventBusSubscriber`, métodos `@SubscribeEvent` | Forge los localiza por anotaciones |
| `balance/*` (`BalanceData`, `Range`) | Gson lee y escribe por nombre de campo: renombrar rompe los JSON del usuario |
| Registros y claves NBT, ids de paquetes | Son cadenas y orden: no cambiar orden de `Network`; las cadenas no se tocan |
| Clases de Minecraft, Forge y GeckoLib, y sobrescrituras de sus métodos | Nombres SRG que resuelve el runtime |
| `META-INF/`, `pack.mcmeta`, `assets/`, `data/` | Recursos que Forge lee por ruta |
| Lo que use reflexión | Hoy una referencia en `shadow/ShadowAI` |
| Mixins | El mod no tiene hoy; si se añaden, sus clases tampoco |

Candidato razonable: renombrar solo clases internas de lógica (`rpg/ability`, `shadow`) sin eliminar código ni optimizar. Lo que más protege no es esto, sino lo de abajo.

## Multijugador

Protege más que ofuscar. El mod ya sigue "el servidor manda" (`umbral-sombras-codigo/CLAUDE.md`):

- Daño, objetivos, recursos, recargas, progresión y reglas de dominio se calculan en el servidor.
- El cliente envía intenciones (usar habilidad, elegir clase) y el servidor valida clase, coste, recarga y sesión. No envía posiciones ni valores.
- El balance vive en los JSON del servidor; el cliente no los lee.

Un cliente modificado puede cambiar lo que ve, no lo que ocurre. No se usa la versión que declara un cliente como prueba de legitimidad, porque se puede falsificar. El servidor es de quien lo aloje: no hay ninguno obligatorio.
