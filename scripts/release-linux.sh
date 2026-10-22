#!/bin/sh
# Builds the Linux release of Azoth Launcher with your server address baked in.
#
# The server address is not part of the source code. Like scripts/release.ps1,
# this script reads it from release.env (gitignored; start from
# release.env.example):
#
#     GATEWAY_HOST=your.server.host
#     GATEWAY_PORT=6000
#
# It builds azoth-patcher.exe (the Windows helper that runs in Wine), bakes its
# sha256 into the launcher, builds the launcher (Wails + WebKitGTK 4.1) and
# packs both into build/bin:
#
#     AzothLauncher-linux-x86_64.tar.gz      always
#     AzothLauncher-x86_64.AppImage          when appimagetool is in PATH
#
# each with a .sha256 file ("<hex>  <name>") next to it.
#
#     scripts/release-linux.sh               release build (needs release.env)
#     scripts/release-linux.sh --no-server   build without a server (Play refuses)
#
# Needs Go 1.26+, a C compiler, pkg-config and the webkit2gtk-4.1 and gtk+-3.0
# development packages. Build on an old distribution (Ubuntu 22.04) so the
# binary runs on most others.
set -eu

fail() {
	echo "release: $*" >&2
	exit 1
}

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

# ---- release.env --------------------------------------------------------------
ldflags="-s -w"
case "${1:-}" in
--no-server) ;;
"")
	[ -f release.env ] || fail "release.env not found. Copy release.env.example to release.env and put your server in it."
	gateway_host=$(sed -n 's/^[[:space:]]*GATEWAY_HOST[[:space:]]*=[[:space:]]*"\{0,1\}\([^"]*\)"\{0,1\}[[:space:]]*$/\1/p' release.env)
	gateway_port=$(sed -n 's/^[[:space:]]*GATEWAY_PORT[[:space:]]*=[[:space:]]*"\{0,1\}\([^"]*\)"\{0,1\}[[:space:]]*$/\1/p' release.env)
	[ -n "$gateway_host" ] || fail "GATEWAY_HOST is missing in release.env."
	[ -n "$gateway_port" ] || fail "GATEWAY_PORT is missing in release.env."
	case "$gateway_host" in
	*[[:space:]:/\\\"\']*) fail "GATEWAY_HOST must be only a host name or IP, without port, scheme or spaces." ;;
	esac
	case "$gateway_port" in
	'' | *[!0-9]*) fail "GATEWAY_PORT must be a number from 1 to 65535." ;;
	esac
	[ "$gateway_port" -ge 1 ] && [ "$gateway_port" -le 65535 ] || fail "GATEWAY_PORT must be a number from 1 to 65535."
	ldflags="$ldflags -X main.gatewayHost=$gateway_host -X main.gatewayPort=$gateway_port"
	;;
*) fail "unknown option: $1 (use --no-server or nothing)" ;;
esac

out=build/bin/linux
rm -rf "$out"
mkdir -p "$out"

# ---- helper -------------------------------------------------------------------
echo "Building azoth-patcher.exe..."
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$out/azoth-patcher.exe" ./cmd/azoth-patcher
patcher_sha=$(sha256sum "$out/azoth-patcher.exe" | cut -d' ' -f1)

# ---- launcher -----------------------------------------------------------------
echo "Building azoth-launcher..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 -trimpath \
	-ldflags "$ldflags -X main.patcherSHA256=$patcher_sha" -o "$out/azoth-launcher" .

cp build/linux/azoth-launcher.desktop "$out/"
cp build/appicon.png "$out/azoth-launcher.png"
cp LICENSE "$out/"

# ---- packages -----------------------------------------------------------------
checksum() {
	(cd build/bin && sha256sum "$1" >"$1.sha256")
	echo "  $1  $(cut -d' ' -f1 "build/bin/$1.sha256")"
}

tarball=AzothLauncher-linux-x86_64.tar.gz
tar -C build/bin -czf "build/bin/$tarball" --transform 's,^linux,AzothLauncher,' linux

echo ""
echo "Done."
checksum "$tarball"

appimagetool=$(command -v appimagetool || true)
if [ -z "$appimagetool" ]; then
	echo "  (no AppImage: appimagetool is not in PATH)"
	exit 0
fi
appdir=build/bin/AppDir
rm -rf "$appdir"
mkdir -p "$appdir/usr/bin"
cp "$out/azoth-launcher" "$out/azoth-patcher.exe" "$appdir/usr/bin/"
cp "$out/azoth-launcher.desktop" "$out/azoth-launcher.png" "$appdir/"
cat >"$appdir/AppRun" <<'EOF'
#!/bin/sh
here=$(dirname "$(readlink -f "$0")")
exec "$here/usr/bin/azoth-launcher" "$@"
EOF
chmod 755 "$appdir/AppRun"
appimage=AzothLauncher-x86_64.AppImage
ARCH=x86_64 "$appimagetool" --no-appstream "$appdir" "build/bin/$appimage" >/dev/null
rm -rf "$appdir"
checksum "$appimage"
