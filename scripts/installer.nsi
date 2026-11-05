; DBA Windows 安装包（NSIS 3）。由 scripts/build.sh setup 或 build.ps1 -Target setup 在 scripts/ 目录内调用：makensis installer.nsi
; 按用户安装到 %LOCALAPPDATA%\Programs\DBA，不需要管理员权限；卸载时保留 %APPDATA%\DataBaseAgent 工作空间。
Unicode true
SetCompressor /SOLID lzma
!define VERSION "0.1.0"
!define UNINST "Software\Microsoft\Windows\CurrentVersion\Uninstall\DBA"
!define WEBVIEW2 "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
Name "DBA ${VERSION}"
OutFile "../bin/DBA-Setup.exe"
InstallDir "$LOCALAPPDATA\Programs\DBA"
InstallDirRegKey HKCU "${UNINST}" "InstallLocation"
RequestExecutionLevel user

!include "MUI2.nsh"
!define MUI_FINISHPAGE_RUN "$INSTDIR\DBA.exe"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Section "Install"
  ; WebView2 Runtime：先查按机器安装，再查按用户安装
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\${WEBVIEW2}" "pv"
  StrCmp $0 "" 0 +2
  ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\${WEBVIEW2}" "pv"
  StrCmp $0 "" 0 webview_ok
    MessageBox MB_YESNO|MB_ICONEXCLAMATION "未检测到 Microsoft Edge WebView2 Runtime，DBA 需要它才能显示界面。$\n现在打开下载页面吗？（安装会继续）" IDNO webview_ok
    ExecShell "open" "https://developer.microsoft.com/microsoft-edge/webview2/"
  webview_ok:
  SetOutPath "$INSTDIR"
  File "../bin/DBA.exe"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateDirectory "$SMPROGRAMS\DBA"
  CreateShortcut "$SMPROGRAMS\DBA\DBA.lnk" "$INSTDIR\DBA.exe"
  CreateShortcut "$SMPROGRAMS\DBA\卸载 DBA.lnk" "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "${UNINST}" "DisplayName" "DBA · 数据库构建工作台"
  WriteRegStr HKCU "${UNINST}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST}" "DisplayIcon" "$INSTDIR\DBA.exe"
  WriteRegStr HKCU "${UNINST}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST}" "UninstallString" "$\"$INSTDIR\Uninstall.exe$\""
  WriteRegDWORD HKCU "${UNINST}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\DBA.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\DBA\DBA.lnk"
  Delete "$SMPROGRAMS\DBA\卸载 DBA.lnk"
  RMDir "$SMPROGRAMS\DBA"
  DeleteRegKey HKCU "${UNINST}"
SectionEnd
