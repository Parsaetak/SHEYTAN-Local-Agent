; ============================================================================
; SHEYTAN-LA — SHEYTAN Local Agent — Windows installer (NSIS, v1.2.0)
; ============================================================================
; Built by CI (.github/workflows/build-desktop.yml) on the windows runner:
;
;   makensis -DVERSION=<ver> -DEXE=SHEYTAN-LA.exe -DBUILDDIR=<staging> installer.nsi
;
; Contract:
;   - installs the APPLICATION ONLY (exe + license + readme); models are
;     NEVER bundled and NEVER deleted;
;   - per-machine install to $PROGRAMFILES64\SHEYTAN-LA;
;   - user data dir (%LOCALAPPDATA%\SHEYTAN-LA) is declared via the
;     SHEYTAN_DATA_DIR environment variable — the app's own portable
;     DataDir override, so existing config machinery reads it unchanged;
;   - AppUserModelID Parsaetak.SHEYTAN-LA registered under HKCU classes so
;     taskbar/notifications resolve the identity;
;   - version-aware upgrades, clean uninstall, user data untouched;
;   - NO Authenticode claim: signing runs later in CI when credentials
;     exist (signtool step is additive, not part of this script).
; ============================================================================

Unicode true
ManifestDPIAware true

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef EXE
  !define EXE "SHEYTAN-LA.exe"
!endif
!ifndef BUILDDIR
  !define BUILDDIR "..\dist\windows\app\SHEYTAN-LA"
!endif

!include "MUI2.nsh"
!include "FileFunc.nsh"

!define PRODUCT       "SHEYTAN-LA"
!define DESCRIPTION   "SHEYTAN Local Agent"
!define PUBLISHER     "Parsaetak"
!define AUMID         "Parsaetak.SHEYTAN-LA"
!define REGKEY        "Software\Parsaetak\SHEYTAN-LA"
!define UNINSTKEY     "Software\Microsoft\Windows\CurrentVersion\Uninstall\SHEYTAN-LA"

Name "${DESCRIPTION} v${VERSION}"
OutFile "..\..\dist\SHEYTAN-LA-v${VERSION}-windows-x64-installer.exe"
InstallDir "$PROGRAMFILES64\SHEYTAN-LA"
InstallDirRegKey HKLM "${REGKEY}" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${PRODUCT}"
VIAddVersionKey "FileDescription" "${DESCRIPTION}"
VIAddVersionKey "CompanyName" "${PUBLISHER}"
VIAddVersionKey "InternalName" "${PRODUCT}"
VIAddVersionKey "OriginalFilename" "SHEYTAN-LA-v${VERSION}-windows-x64-installer.exe"
VIAddVersionKey "FileVersion" "${VERSION}.0"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "(c) 2024-2026 Parsaetak. All rights reserved."

!define MUI_ICON   "..\..\build\sheytan.ico"
!define MUI_UNICON "..\..\build\sheytan.ico"
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${DESCRIPTION}"
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetOutPath "$INSTDIR"

  ; Application payload (never models).
  File "${BUILDDIR}\${EXE}"
  File /nonfatal "${BUILDDIR}\LICENSE"
  File /nonfatal "${BUILDDIR}\README.md"
  File /nonfatal "${BUILDDIR}\SIGNATURE"
  File "..\..\build\sheytan.ico"

  ; Version-aware upgrade bookkeeping.
  WriteRegStr HKLM "${REGKEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "${REGKEY}" "Version" "${VERSION}"
  WriteRegStr HKLM "${REGKEY}" "Publisher" "${PUBLISHER}"

  ; User data location — the app's documented SHEYTAN_DATA_DIR override.
  ; Existing installs keep their data; the directory is created on first run.
  WriteRegExpandStr HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "SHEYTAN_DATA_DIR" "%LOCALAPPDATA%\SHEYTAN-LA"

  ; Windows application identity (AppUserModelID) — notifications and
  ; taskbar grouping resolve to SHEYTAN-LA.
  WriteRegStr HKCU "Software\Classes\AppUserModelId\${AUMID}" "DisplayName" "${DESCRIPTION}"
  WriteRegStr HKCU "Software\Classes\AppUserModelId\${AUMID}" "IconUri" "$INSTDIR\sheytan.ico"

  ; Start Menu shortcut.
  CreateDirectory "$SMPROGRAMS\${PRODUCT}"
  CreateShortcut "$SMPROGRAMS\${PRODUCT}\${DESCRIPTION}.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\sheytan.ico"
  CreateShortcut "$SMPROGRAMS\${PRODUCT}\Uninstall ${DESCRIPTION}.lnk" "$INSTDIR\Uninstall.exe"

  ; Optional desktop shortcut.
  CreateShortcut "$DESKTOP\${DESCRIPTION}.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\sheytan.ico"

  ; Add/Remove Programs entry.
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayName" "${DESCRIPTION} v${VERSION}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINSTKEY}" "Publisher" "${PUBLISHER}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\sheytan.ico"
  WriteRegStr HKLM "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTKEY}" "UninstallString" "$INSTDIR\Uninstall.exe"
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoRepair" 1

  ; Installed size for ARP.
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKLM "${UNINSTKEY}" "EstimatedSize" $0

  WriteUninstaller "$INSTDIR\Uninstall.exe"
SectionEnd

Section "Uninstall"
  ; Remove the application — and NOTHING else. models/, workspace/,
  ; sessions/ and config live under %LOCALAPPDATA%\SHEYTAN-LA (or a
  ; portable folder) and are deliberately PRESERVED.
  Delete "$INSTDIR\${EXE}"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\SIGNATURE"
  Delete "$INSTDIR\sheytan.ico"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${PRODUCT}\${DESCRIPTION}.lnk"
  Delete "$SMPROGRAMS\${PRODUCT}\Uninstall ${DESCRIPTION}.lnk"
  RMDir "$SMPROGRAMS\${PRODUCT}"
  Delete "$DESKTOP\${DESCRIPTION}.lnk"

  DeleteRegKey HKCU "Software\Classes\AppUserModelId\${AUMID}"
  DeleteRegKey HKLM "${UNINSTKEY}"

  ; Keep HKLM "${REGKEY}" so a repaired install remembers the directory.
  ReadRegStr $0 HKLM "${REGKEY}" "InstallDir"
  StrCmp $0 "" 0 +2
    DeleteRegKey HKLM "${REGKEY}"

  ; NOTE: the machine SHEYTAN_DATA_DIR variable is intentionally left in
  ; place so user data stays discoverable by remaining installs; removing
  ; it would orphan models for users with multiple copies.
SectionEnd
