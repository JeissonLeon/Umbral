; Instalador de Umbral Launcher. Se compila con build.sh (makensis).
; Instala para el usuario actual, sin pedir permisos de administrador.

Unicode true
!include "MUI2.nsh"
!include "WinVer.nsh"
!include "x64.nsh"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!define APPNAME "Umbral"
!define EXE "UmbralLauncher.exe"
!define UPDATER "umbral-updater.exe"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Umbral"
; Clave de registro del runtime WebView2 (Evergreen)
!define WV2KEY "SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "${APPNAME}"
OutFile "..\dist\UmbralSetup.exe"
InstallDir "$LOCALAPPDATA\Programs\Umbral"
InstallDirRegKey HKCU "${UNINSTKEY}" "InstallLocation"
RequestExecutionLevel user
SetCompressor /SOLID lzma
BrandingText "Umbral Launcher ${VERSION}"

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${APPNAME}"
VIAddVersionKey "FileDescription" "Instalador de Umbral Launcher"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Umbral"

!define MUI_ICON "..\assets\icon.ico"
!define MUI_UNICON "..\assets\icon.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEFINISHPAGE_BITMAP "..\assets\instalador-lateral.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "..\assets\instalador-lateral.bmp"
!define MUI_WELCOMEPAGE_TITLE "Instalar Umbral Launcher"
!define MUI_WELCOMEPAGE_TEXT "Este asistente instala el launcher de SylvanΩr 3.$\r$\n$\r$\nAl abrirlo, descargará Minecraft 1.20.1, Java 17, Forge y los mods del modpack (aproximadamente 1 GB). Después se actualiza solo.$\r$\n$\r$\nPara jugar necesitas tu propia copia de Minecraft: Java Edition."
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Abrir Umbral ahora"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "Spanish"

Function .onInit
  ; Comprueba los requisitos antes de tocar nada.
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_OK|MB_ICONSTOP "Umbral Launcher necesita Windows 10 o superior." /SD IDOK
    Abort
  ${EndIf}
  ${IfNot} ${RunningX64}
    MessageBox MB_OK|MB_ICONSTOP "Umbral Launcher necesita Windows de 64 bits." /SD IDOK
    Abort
  ${EndIf}
  ; Cierra el launcher si está abierto para poder reemplazarlo.
  nsExec::Exec 'taskkill /IM ${EXE}'
  nsExec::Exec 'taskkill /IM ${UPDATER}'
  Sleep 500
FunctionEnd

Section "Umbral" SecMain
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "..\dist\${EXE}"
  File "..\dist\${UPDATER}"
  File "..\assets\icon.ico"
  ; Restos de una actualización anterior interrumpida.
  Delete "$INSTDIR\${EXE}.bak"
  Delete "$INSTDIR\${EXE}.new"
  Delete "$INSTDIR\${UPDATER}.new"
  WriteUninstaller "$INSTDIR\Desinstalar.exe"

  CreateDirectory "$SMPROGRAMS\Umbral"
  CreateShortcut "$SMPROGRAMS\Umbral\Umbral.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\icon.ico"
  CreateShortcut "$SMPROGRAMS\Umbral\Desinstalar Umbral.lnk" "$INSTDIR\Desinstalar.exe"
  CreateShortcut "$DESKTOP\Umbral.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\icon.ico"

  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "Umbral Launcher"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "Umbral"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\icon.ico"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Desinstalar.exe"'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" 12000

  ; Windows 10 y 11 suelen traer WebView2; si falta, se ofrece descargarlo.
  ReadRegStr $0 HKLM "${WV2KEY}" "pv"
  StrCmp $0 "" 0 wv2ok
  SetRegView 64
  ReadRegStr $0 HKLM "${WV2KEY}" "pv"
  SetRegView 32
  StrCmp $0 "" 0 wv2ok
  ReadRegStr $0 HKCU "${WV2KEY}" "pv"
  StrCmp $0 "" 0 wv2ok
  MessageBox MB_YESNO|MB_ICONINFORMATION "Umbral necesita Microsoft Edge WebView2, que no está en este equipo.$\r$\n$\r$\n¿Abrir la página de descarga de Microsoft?" /SD IDNO IDNO wv2ok
  ExecShell "open" "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
  wv2ok:
SectionEnd

Section "Uninstall"
  nsExec::Exec 'taskkill /IM ${EXE}'
  nsExec::Exec 'taskkill /IM ${UPDATER}'
  Sleep 500
  Delete "$INSTDIR\${EXE}"
  Delete "$INSTDIR\${EXE}.bak"
  Delete "$INSTDIR\${EXE}.new"
  Delete "$INSTDIR\${UPDATER}"
  Delete "$INSTDIR\${UPDATER}.new"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\Desinstalar.exe"
  RMDir "$INSTDIR"
  Delete "$DESKTOP\Umbral.lnk"
  RMDir /r "$SMPROGRAMS\Umbral"
  DeleteRegKey HKCU "${UNINSTKEY}"

  MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "¿Borrar también los datos del juego?$\r$\n$\r$\nIncluye mundos de un jugador, capturas y ajustes ($APPDATA\umbral). Si elegiste otra carpeta en Ajustes, esa no se borra. Si eliges No, se conservan para una próxima instalación." /SD IDNO IDNO keepdata
  RMDir /r "$APPDATA\umbral"
  keepdata:
SectionEnd
