#!/usr/bin/env bash
# 用法：scripts/build.sh [windows|darwin|linux|all|setup|dmg] [--debug]
# 默认构建当前平台。产物放在 bin/：DBA.exe、DBA.app、DBA；setup 生成 DBA-Setup.exe（需要 NSIS），dmg 生成 DBA.dmg（仅 macOS）。
# 本机平台用 cgo（稳定路径，需要 Xcode 命令行工具或 GTK/WebKit 开发包）；
# 交叉编译其他平台，或设置 PUREGO=1，则用 Wails 的 purego 后端，不需要 C 工具链（实验特性）。
# TAGS=gtk3 可追加构建标签（Linux 改用 GTK 3 + WebKit2GTK 4.1）。
set -euo pipefail
cd "$(dirname "$0")/.."
target=${1:-$(go env GOOS)}
debug=0; [ "${2:-}" = "--debug" ] && debug=1
host=$(go env GOOS)

build() { # build <os> <arch> <输出路径>
  local os=$1 arch=$2 out=$3 tags=production ldflags='-s -w' cgo=1
  [ $debug = 1 ] && { tags=''; ldflags=''; }
  [ -n "${TAGS:-}" ] && tags="$TAGS${tags:+,$tags}"
  [ "$os" = windows ] && ldflags="$ldflags -H windowsgui"
  if [ "$os" = windows ] || [ "$os" != "$host" ] || [ "${PUREGO:-0}" = 1 ]; then
    cgo=0; [ "$os" != windows ] && tags="purego${tags:+,$tags}"
  fi
  echo "==> $os/$arch -> $out (CGO_ENABLED=$cgo tags=${tags:-无})"
  MACOSX_DEPLOYMENT_TARGET=11.0 CGO_CFLAGS=-mmacosx-version-min=11.0 CGO_LDFLAGS=-mmacosx-version-min=11.0 \
  CGO_ENABLED=$cgo GOOS=$os GOARCH=$arch go build ${tags:+-tags "$tags"} -ldflags "$ldflags" -o "$out" .
}

mac_app() { # 组装最小 .app 包
  local app=bin/DBA.app/Contents
  mkdir -p "$app/MacOS"
  build darwin "${GOARCH:-$( [ "$host" = darwin ] && go env GOARCH || echo arm64 )}" "$app/MacOS/DBA"
  cat > "$app/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>DBA</string>
  <key>CFBundleDisplayName</key><string>DBA</string>
  <key>CFBundleExecutable</key><string>DBA</string>
  <key>CFBundleIdentifier</key><string>org.databaseagent.dba</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>0.1.0</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
}

mkdir -p bin
case $target in
  windows) build windows "${GOARCH:-amd64}" bin/DBA.exe ;;
  linux)   build linux "${GOARCH:-amd64}" bin/DBA ;;
  darwin|macos) mac_app ;;
  all) build windows amd64 bin/DBA.exe; build linux amd64 bin/DBA; mac_app ;;
  setup)
    build windows "${GOARCH:-amd64}" bin/DBA.exe
    command -v makensis >/dev/null || { echo "需要 NSIS 3：macOS brew install makensis，Debian/Ubuntu apt install nsis，Windows 见 nsis.sourceforge.io" >&2; exit 2; }
    # ponytail: POSIX 版 makensis 按 locale 解码脚本里的中文；没有 en_US.UTF-8 的系统换成 C.UTF-8
    (cd scripts && LC_ALL=en_US.UTF-8 makensis -V2 installer.nsi) && echo "==> bin/DBA-Setup.exe" ;;
  dmg)
    [ "$host" = darwin ] || { echo "dmg 只能在 macOS 上用 hdiutil 生成" >&2; exit 2; }
    mac_app
    rm -rf bin/dmg bin/DBA.dmg && mkdir -p bin/dmg && cp -R bin/DBA.app bin/dmg/ && ln -s /Applications bin/dmg/Applications
    hdiutil create -quiet -volname DBA -srcfolder bin/dmg -ov -format UDZO bin/DBA.dmg && rm -rf bin/dmg && echo "==> bin/DBA.dmg" ;;
  *) echo "未知目标：$target（可选 windows、darwin、linux、all、setup、dmg）" >&2; exit 2 ;;
esac
