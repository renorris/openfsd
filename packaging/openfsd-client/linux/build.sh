#!/usr/bin/env bash
# Build Linux .deb (and portable .tar.gz) for openfsd-client.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../common.sh
source "${SCRIPT_DIR}/../common.sh"

GOARCH="${GOARCH:-$(uname -m)}"
case "${GOARCH}" in
  x86_64|amd64) GOARCH=amd64; NFPM_ARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64; NFPM_ARCH=arm64 ;;
  *) NFPM_ARCH="${GOARCH}" ;;
esac

ensure_icons
BIN_DIR="${DIST_ROOT}/bin/linux-${GOARCH}"
BIN_PATH="${BIN_DIR}/${BIN_NAME}"
build_binary "${BIN_PATH}" linux "${GOARCH}"

# Ensure icons exist for package (fallback: copy 512 for all)
ASSETS="${PKG_ROOT}/assets"
for s in 128 256 512; do
  if [[ ! -f "${ASSETS}/icon-${s}.png" ]] && [[ -f "${ASSETS}/icon-512.png" ]]; then
    if command -v magick >/dev/null 2>&1; then
      magick "${ASSETS}/icon-512.png" -resize "${s}x${s}" "${ASSETS}/icon-${s}.png"
    elif command -v convert >/dev/null 2>&1; then
      convert "${ASSETS}/icon-512.png" -resize "${s}x${s}" "${ASSETS}/icon-${s}.png"
    else
      cp "${ASSETS}/icon-512.png" "${ASSETS}/icon-${s}.png"
    fi
  fi
done

DESKTOP_FILE="${SCRIPT_DIR}/openfsd-client.desktop"
ICON_128="${ASSETS}/icon-128.png"
ICON_256="${ASSETS}/icon-256.png"
ICON_512="${ASSETS}/icon-512.png"

# nfpm
if ! command -v nfpm >/dev/null 2>&1; then
  echo "==> installing nfpm"
  go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.3
  export PATH="$(go env GOPATH)/bin:${PATH}"
fi

STAGING="${DIST_ROOT}/staging/linux"
mkdir -p "${STAGING}"
NFPM_CFG="${STAGING}/nfpm.yaml"
export VERSION NFPM_ARCH BIN_PATH DESKTOP_FILE ICON_128 ICON_256 ICON_512
# envsubst or sed
if command -v envsubst >/dev/null 2>&1; then
  envsubst < "${SCRIPT_DIR}/nfpm.yaml.in" > "${NFPM_CFG}"
else
  sed \
    -e "s|\${VERSION}|${VERSION}|g" \
    -e "s|\${NFPM_ARCH}|${NFPM_ARCH}|g" \
    -e "s|\${BIN_PATH}|${BIN_PATH}|g" \
    -e "s|\${DESKTOP_FILE}|${DESKTOP_FILE}|g" \
    -e "s|\${ICON_128}|${ICON_128}|g" \
    -e "s|\${ICON_256}|${ICON_256}|g" \
    -e "s|\${ICON_512}|${ICON_512}|g" \
    "${SCRIPT_DIR}/nfpm.yaml.in" > "${NFPM_CFG}"
fi

DEB_OUT="${DIST_ROOT}/${BIN_NAME}_${VERSION}_${NFPM_ARCH}.deb"
echo "==> nfpm package deb -> ${DEB_OUT}"
nfpm package --config "${NFPM_CFG}" --packager deb --target "${DEB_OUT}"

# Portable tarball (binary + desktop file + readme)
PORTABLE_DIR="${STAGING}/portable"
rm -rf "${PORTABLE_DIR}"
mkdir -p "${PORTABLE_DIR}"
cp "${BIN_PATH}" "${PORTABLE_DIR}/${BIN_NAME}"
cp "${DESKTOP_FILE}" "${PORTABLE_DIR}/"
cat > "${PORTABLE_DIR}/README.txt" <<EOF
${PRODUCT_NAME} ${VERSION}

Run: ./${BIN_NAME}
Config: ~/.config/openfsd-client/

System package (recommended on Debian/Ubuntu):
  sudo apt install ./${BIN_NAME}_${VERSION}_${NFPM_ARCH}.deb
EOF
TAR_OUT="${DIST_ROOT}/${BIN_NAME}-${VERSION}-linux-${GOARCH}.tar.gz"
tar -C "${PORTABLE_DIR}" -czf "${TAR_OUT}" .

echo "==> Linux artifacts:"
ls -la "${DEB_OUT}" "${TAR_OUT}"
