#!/usr/bin/env bash
# Build macOS .app + .pkg + .dmg for openfsd-client.
# The .app is a thin wrapper: Info.plist + the plain Go binary (no Fyne package).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../common.sh
source "${SCRIPT_DIR}/../common.sh"

GOARCH="${GOARCH:-$(uname -m)}"
case "${GOARCH}" in
  x86_64) GOARCH=amd64 ;;
  arm64|aarch64) GOARCH=arm64 ;;
esac

ensure_icons
BIN_DIR="${DIST_ROOT}/bin/darwin-${GOARCH}"
BIN_PATH="${BIN_DIR}/${BIN_NAME}"
build_binary "${BIN_PATH}" darwin "${GOARCH}"

APP_BUNDLE_NAME="${PRODUCT_NAME}.app"
STAGING="${DIST_ROOT}/staging/macos"
APP_ROOT="${STAGING}/${APP_BUNDLE_NAME}"
rm -rf "${STAGING}"
mkdir -p "${APP_ROOT}/Contents/MacOS" "${APP_ROOT}/Contents/Resources"

# Install binary as the app executable (direct Go binary).
cp "${BIN_PATH}" "${APP_ROOT}/Contents/MacOS/${BIN_NAME}"
chmod +x "${APP_ROOT}/Contents/MacOS/${BIN_NAME}"

# Info.plist
sed -e "s|@PRODUCT_NAME@|${PRODUCT_NAME}|g" \
    -e "s|@BIN_NAME@|${BIN_NAME}|g" \
    -e "s|@APP_ID@|${APP_ID}|g" \
    -e "s|@VERSION@|${VERSION}|g" \
    "${SCRIPT_DIR}/Info.plist.in" > "${APP_ROOT}/Contents/Info.plist"

# AppIcon.icns
ICONSET="${STAGING}/AppIcon.iconset"
rm -rf "${ICONSET}"
mkdir -p "${ICONSET}"
if [[ -f "${PKG_ROOT}/assets/icon-512.png" ]]; then
  for pair in \
    "icon_16x16.png:16" "diana.r@example.org:32" \
    "icon_32x32.png:32" "ivan.p@example.net:64" \
    "icon_128x128.png:128" "wendy.h@example.net:256" \
    "icon_256x256.png:256" "wendy.h@example.net:512" \
    "icon_512x512.png:512" "walt.e@example.net:512"; do
    name="${pair%%:*}"
    size="${pair##*:}"
    if command -v sips >/dev/null 2>&1; then
      sips -z "${size}" "${size}" "${PKG_ROOT}/assets/icon-512.png" --out "${ICONSET}/${name}" >/dev/null
    elif command -v magick >/dev/null 2>&1; then
      magick "${PKG_ROOT}/assets/icon-512.png" -resize "${size}x${size}" "${ICONSET}/${name}"
    fi
  done
  # 1024 for @2x 512
  if command -v sips >/dev/null 2>&1; then
    sips -z 1024 1024 "${PKG_ROOT}/assets/icon-512.png" --out "${ICONSET}/walt.e@example.net" >/dev/null 2>&1 || true
  fi
  iconutil -c icns "${ICONSET}" -o "${APP_ROOT}/Contents/Resources/AppIcon.icns"
fi

# Pkgroot for productbuild: install into /Applications
PKGROOT="${STAGING}/pkgroot"
rm -rf "${PKGROOT}"
mkdir -p "${PKGROOT}/Applications"
cp -R "${APP_ROOT}" "${PKGROOT}/Applications/"

PKG_OUT="${DIST_ROOT}/${BIN_NAME}-${VERSION}-darwin-${GOARCH}.pkg"
COMPONENT_PKG="${STAGING}/component.pkg"
pkgbuild \
  --root "${PKGROOT}" \
  --identifier "${APP_ID}" \
  --version "${VERSION}" \
  --install-location "/" \
  "${COMPONENT_PKG}"

# Distribution XML for a standard Installer.app experience
DIST_XML="${STAGING}/distribution.xml"
cat > "${DIST_XML}" <<EOF
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
    <title>${PRODUCT_NAME}</title>
    <organization>com.openfsd</organization>
    <options customize="never" require-scripts="false" hostArchitectures="${GOARCH/amd64/x86_64}"/>
    <welcome file="welcome.html" mime-type="text/html"/>
    <license file="license.txt" mime-type="text/plain"/>
    <choices-outline>
        <line choice="default">
            <line choice="com.openfsd.client-setup"/>
        </line>
    </choices-outline>
    <choice id="default"/>
    <choice id="com.openfsd.client-setup" visible="false">
        <pkg-ref id="com.openfsd.client-setup"/>
    </choice>
    <pkg-ref id="com.openfsd.client-setup" version="${VERSION}" onConclusion="none">${BIN_NAME}-component.pkg</pkg-ref>
</installer-gui-script>
EOF
# productbuild wants pkg-ref path relative — copy component with expected name
cp "${COMPONENT_PKG}" "${STAGING}/${BIN_NAME}-component.pkg"

WELCOME="${STAGING}/welcome.html"
cat > "${WELCOME}" <<EOF
<!DOCTYPE html>
<html><body style="font-family: -apple-system, sans-serif; padding: 12px;">
<h2>${PRODUCT_NAME}</h2>
<p>This installer places <strong>${PRODUCT_NAME}</strong> in <code>/Applications</code>.</p>
<p>Settings are stored in your home folder under<br>
<code>~/Library/Application Support/openfsd-client/</code><br>
and are not removed when you uninstall the app.</p>
</body></html>
EOF
cp "${REPO_ROOT}/LICENSE" "${STAGING}/license.txt"

# Simpler reliable path: just pkgbuild output as the deliverable (Installer opens it).
# productbuild can fail on relative refs; keep component.pkg as primary + wrap if possible.
if productbuild \
  --distribution "${DIST_XML}" \
  --package-path "${STAGING}" \
  --resources "${STAGING}" \
  "${PKG_OUT}" 2>/dev/null; then
  echo "==> productbuild OK: ${PKG_OUT}"
else
  echo "==> productbuild fallback: shipping component pkg"
  cp "${COMPONENT_PKG}" "${PKG_OUT}"
fi

# DMG: familiar drag-to-Applications
DMG_STAGE="${STAGING}/dmg"
rm -rf "${DMG_STAGE}"
mkdir -p "${DMG_STAGE}"
cp -R "${APP_ROOT}" "${DMG_STAGE}/"
ln -s /Applications "${DMG_STAGE}/Applications"
DMG_OUT="${DIST_ROOT}/${BIN_NAME}-${VERSION}-darwin-${GOARCH}.dmg"
rm -f "${DMG_OUT}"
hdiutil create -volname "${PRODUCT_NAME}" -srcfolder "${DMG_STAGE}" \
  -ov -format UDZO "${DMG_OUT}"

# Bare binary tarball for CLI / power users
TAR_OUT="${DIST_ROOT}/${BIN_NAME}-${VERSION}-darwin-${GOARCH}.tar.gz"
tar -C "${BIN_DIR}" -czf "${TAR_OUT}" "${BIN_NAME}"

echo "==> macOS artifacts:"
ls -la "${PKG_OUT}" "${DMG_OUT}" "${TAR_OUT}"
