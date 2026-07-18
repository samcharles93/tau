#!/usr/bin/env bash
set -euo pipefail

REPO="samcharles93/tau"
BASE_URL="${TAU_BASE_URL:-https://github.com/${REPO}/releases}"

# Supported OS/ARCH pairs and the "tau_{version}_{os}_{arch}.{ext}" archive
# name below must match internal/updater/targets.go's SupportedTargets()/
# ArchiveName() exactly - that's the canonical source. This script can't
# import Go code (it runs before Go is even on the machine), so keep the
# two in sync by hand whenever the platform matrix changes.

# ---------- helpers ----------
bold()   { printf '\033[1m%s\033[0m' "$*"; }
green()  { printf '\033[32m%s\033[0m' "$*"; }
yellow() { printf '\033[33m%s\033[0m' "$*"; }
red()    { printf '\033[31m%s\033[0m' "$*"; }

# sha256_of prints the lowercase hex SHA-256 digest of the given file.
sha256_of() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    echo "$(red 'Error'): no sha256sum or shasum available to verify checksum" >&2
    return 1
  fi
}

# check_prerequisites verifies the commands run_install depends on are
# present before any network call is made, so a missing dependency fails
# fast with a clear message instead of a confusing curl/tar error mid-run.
check_prerequisites() {
  local missing=()
  command -v curl >/dev/null 2>&1 || missing+=("curl")
  command -v tar >/dev/null 2>&1 || missing+=("tar")

  if [ ${#missing[@]} -gt 0 ]; then
    echo "$(red 'Error'): missing required command(s): ${missing[*]}" >&2
    echo "  Install them and re-run this script." >&2
    exit 1
  fi
}

# verify_checksum checks that $file's SHA-256 matches the entry for $name
# in the checksums.txt at $checksums_file. It never touches anything
# outside $file/$checksums_file - callers are responsible for only
# replacing an installed binary after this succeeds.
verify_checksum() {
  local file="$1" name="$2" checksums_file="$3"

  if [ ! -f "$checksums_file" ]; then
    echo "$(red 'Error'): checksums.txt was not downloaded" >&2
    return 1
  fi

  local expected
  expected=$(awk -v f="$name" '$2 == f {print $1; exit}' "$checksums_file")
  if [ -z "$expected" ]; then
    echo "$(red 'Error'): no checksum entry for $name in checksums.txt" >&2
    return 1
  fi

  local actual
  actual=$(sha256_of "$file") || return 1

  if [ "$expected" != "$actual" ]; then
    echo "$(red 'Error'): checksum mismatch for $name" >&2
    echo "  expected: $expected" >&2
    echo "  actual:   $actual" >&2
    return 1
  fi
}

# extract_binary extracts exactly $binary_name from $archive_path into $tmp,
# refusing anything that doesn't match. It never touches the install
# directory - callers are responsible for only replacing an installed
# binary after this and verify_binary_runs both succeed.
extract_binary() {
  local archive_path="$1" tmp="$2" binary_name="$3"

  local listing
  if ! listing=$(tar -tzf "$archive_path" 2>/dev/null); then
    echo "$(red 'Error'): failed to read archive contents - leaving any existing install untouched" >&2
    return 1
  fi

  if ! grep -qx "$binary_name" <<<"$listing"; then
    echo "$(red 'Error'): archive does not contain expected binary '$binary_name' - leaving any existing install untouched" >&2
    return 1
  fi

  tar -xzf "$archive_path" -C "$tmp" "$binary_name"

  if [ ! -f "$tmp/$binary_name" ]; then
    echo "$(red 'Error'): extraction did not produce '$binary_name' - leaving any existing install untouched" >&2
    return 1
  fi
}

# verify_binary_runs runs $binary_path --version to prove the extracted
# file is a working tau binary, not a corrupt or unrelated file, before it
# is ever put anywhere near the install directory.
verify_binary_runs() {
  local binary_path="$1"

  if ! "$binary_path" --version >/dev/null 2>&1; then
    echo "$(red 'Error'): downloaded binary failed to run '--version' - leaving any existing install untouched" >&2
    return 1
  fi
}

# run_install performs OS/arch detection, download, checksum verification,
# and installation. Split out of top level so tests can source this file
# with TAU_INSTALL_TEST_MODE=1 to exercise verify_checksum/sha256_of
# in isolation, without running the real install or touching the network.
run_install() {
  check_prerequisites

  # ---------- detect OS / arch ----------
  local os arch platform
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)

  case "$arch" in
    x86_64|amd64)  arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *)
      echo "$(red 'Error'): unsupported architecture: $arch"
      exit 1
      ;;
  esac

  case "$os" in
    darwin|linux) platform="${os}_${arch}" ;;
    *)
      echo "$(red 'Error'): unsupported OS: $os"
      exit 1
      ;;
  esac

  # ---------- resolve version ----------
  local version
  if [ -n "${TAU_VERSION:-}" ]; then
    version="$TAU_VERSION"
  else
    echo "$(bold '→') Fetching latest release..."
    version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name":' \
      | sed -E 's/.*"([^"]+)".*/\1/')
  fi

  if [ -z "$version" ]; then
    echo "$(red 'Error'): could not determine latest version"
    exit 1
  fi

  local archive download_url checksums_url
  archive="tau_${version#v}_${platform}.tar.gz"
  download_url="${BASE_URL}/download/${version}/${archive}"
  checksums_url="${BASE_URL}/download/${version}/checksums.txt"

  # ---------- install dir ----------
  local install_dir
  if [ -n "${TAU_INSTALL_DIR:-}" ]; then
    install_dir="$TAU_INSTALL_DIR"
    mkdir -p "$install_dir"
  elif [ -w /usr/local/bin ]; then
    install_dir=/usr/local/bin
  elif [ -n "${XDG_BIN_HOME:-}" ]; then
    install_dir="$XDG_BIN_HOME"
  else
    install_dir="$HOME/.local/bin"
    mkdir -p "$install_dir"
  fi

  # ---------- download & verify & extract & install ----------
  echo "$(bold '→') Downloading tau ${version} (${platform})..."
  # Not `local`: the EXIT trap below fires at process exit, after this
  # function has already returned and any local would be gone.
  tmp=$(mktemp -d)
  dest_tmp=""
  trap 'rm -rf "$tmp"; [ -n "$dest_tmp" ] && rm -f "$dest_tmp"' EXIT

  curl -fsSL# -o "$tmp/$archive" "$download_url"
  curl -fsSL -o "$tmp/checksums.txt" "$checksums_url"

  echo "$(bold '→') Verifying checksum..."
  if ! verify_checksum "$tmp/$archive" "$archive" "$tmp/checksums.txt"; then
    echo "$(red 'Error'): checksum verification failed - leaving any existing install untouched"
    exit 1
  fi

  if ! extract_binary "$tmp/$archive" "$tmp" tau; then
    exit 1
  fi
  chmod +x "$tmp/tau"

  echo "$(bold '→') Verifying downloaded binary..."
  if ! verify_binary_runs "$tmp/tau"; then
    exit 1
  fi

  # ---------- atomic install ----------
  # Copy into a temp file in the destination directory, then rename into
  # place. A rename within the same directory is atomic, so there is never
  # a window where ${install_dir}/tau is missing or partially written.
  dest_tmp="${install_dir}/.tau.tmp.$$"
  cp "$tmp/tau" "$dest_tmp"
  chmod +x "$dest_tmp"
  mv -f "$dest_tmp" "${install_dir}/tau"

  echo ""
  echo "$(green '✓') tau ${version} installed to ${install_dir}/tau"

  # ---------- PATH reminder ----------
  if ! echo "$PATH" | grep -qF "$install_dir"; then
    echo ""
    echo "$(yellow 'Note'): $(bold "$install_dir") is not on your PATH."
    echo "  Add this to your shell config:"
    echo ""
    echo "    export PATH=\"$install_dir:\$PATH\""
    echo ""
  fi

  echo "  Run $(bold 'tau') to get started."
}

if [ "${TAU_INSTALL_TEST_MODE:-0}" != "1" ]; then
  run_install "$@"
fi
