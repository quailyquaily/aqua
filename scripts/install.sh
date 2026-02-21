#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-quailyquaily/aqua}"
VERSION="${1:-${VERSION:-}}"
INSTALL_DIR="${INSTALL_DIR:-}"

if [[ $# -ge 2 ]]; then
  INSTALL_DIR="$2"
fi

resolve_latest_version_tag() {
  local latest_url effective_url tag
  latest_url="https://github.com/${REPO}/releases/latest"
  effective_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "${latest_url}")" || return 1
  tag="${effective_url##*/}"
  tag="${tag%%\?*}"
  if [[ -z "${tag}" || "${tag}" == "latest" ]]; then
    return 1
  fi
  printf '%s\n' "${tag}"
}

if [[ -z "${VERSION}" || "${VERSION}" == "latest" ]]; then
  echo "No version provided, resolving latest release tag..."
  VERSION="$(resolve_latest_version_tag)" || {
    echo "Failed to resolve latest release tag."
    echo "Usage: $0 [version-tag] [install-dir]"
    echo "Example: $0"
    echo "Example: $0 v0.1.0"
    echo "Example: INSTALL_DIR=\$HOME/.local/bin $0"
    exit 1
  }
  echo "Resolved latest tag: ${VERSION}"
fi

VERSION_TAG="${VERSION}"
if [[ "${VERSION_TAG}" != v* ]]; then
  VERSION_TAG="v${VERSION_TAG}"
fi
ASSET_VERSION="${VERSION_TAG#v}"

OS="$(uname -s)"
case "${OS}" in
  Linux) OS="linux" ;;
  Darwin) OS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *)
    echo "Unsupported OS: ${OS} (supported: Linux, Darwin, Windows via Git Bash/MSYS2/Cygwin)"
    exit 1
    ;;
esac

if [[ -z "${INSTALL_DIR}" ]]; then
  if [[ "${OS}" == "windows" ]]; then
    INSTALL_DIR="${HOME}/.local/bin"
  else
    INSTALL_DIR="/usr/local/bin"
  fi
fi

ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: ${ARCH} (supported: amd64, arm64)"
    exit 1
    ;;
esac

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

ARCHIVE_EXT="tar.gz"
BIN_NAME="aqua"
if [[ "${OS}" == "windows" ]]; then
  ARCHIVE_EXT="zip"
  BIN_NAME="aqua.exe"
fi

ARCHIVE="${TMP_DIR}/aqua.${ARCHIVE_EXT}"
URL="https://github.com/${REPO}/releases/download/${VERSION_TAG}/aqua_${ASSET_VERSION}_${OS}_${ARCH}.${ARCHIVE_EXT}"

echo "Downloading ${URL}"
curl -fL "${URL}" -o "${ARCHIVE}"
if [[ "${ARCHIVE_EXT}" == "zip" ]]; then
  if command -v unzip >/dev/null 2>&1; then
    unzip -q "${ARCHIVE}" -d "${TMP_DIR}"
  else
    echo "Install failed: unzip is required to extract ${ARCHIVE}"
    exit 1
  fi
else
  tar -xzf "${ARCHIVE}" -C "${TMP_DIR}"
fi

BIN_SRC="${TMP_DIR}/${BIN_NAME}"
if [[ ! -f "${BIN_SRC}" ]]; then
  echo "Install failed: binary ${BIN_NAME} not found in archive"
  exit 1
fi

mkdir -p "${INSTALL_DIR}"
DEST_BIN="${INSTALL_DIR}/${BIN_NAME}"
if [[ "${OS}" == "windows" ]]; then
  if [[ ! -w "${INSTALL_DIR}" ]]; then
    echo "Install failed: ${INSTALL_DIR} is not writable. Set INSTALL_DIR to a writable path."
    exit 1
  fi
  cp -f "${BIN_SRC}" "${DEST_BIN}"
elif [[ -w "${INSTALL_DIR}" ]]; then
  install -m 0755 "${BIN_SRC}" "${DEST_BIN}"
else
  echo "Need elevated permission to write ${INSTALL_DIR}; using sudo"
  sudo install -m 0755 "${BIN_SRC}" "${DEST_BIN}"
fi

echo "Installed to ${DEST_BIN}"
"${DEST_BIN}" version
