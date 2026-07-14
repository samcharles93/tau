#!/usr/bin/env bash
# Tests for install.sh's checksum verification (CAT-114).
#
# Usage: ./install_test.sh
#
# Part 1 sources install.sh in test mode (TAU_INSTALL_TEST_MODE=1) to unit
# test verify_checksum/sha256_of directly, without touching the network.
# Part 2 runs the real run_install end to end against a local static file
# server standing in for GitHub releases, to prove that a failed
# verification leaves any existing installed binary untouched and exits
# nonzero, while a valid one installs cleanly.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

pass=0
fail=0

ok() {
  echo "ok - $1"
  pass=$((pass + 1))
}

not_ok() {
  echo "not ok - $1"
  fail=$((fail + 1))
}

assert_success() {
  local desc="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    ok "$desc"
  else
    not_ok "$desc"
  fi
}

assert_failure() {
  local desc="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    not_ok "$desc (expected failure, got success)"
  else
    ok "$desc"
  fi
}

# ---------- Part 1: unit tests for verify_checksum / sha256_of ----------

unit_tmp=$(mktemp -d)
trap 'rm -rf "$unit_tmp"' EXIT

export TAU_INSTALL_TEST_MODE=1
# shellcheck source=install.sh
source "$SCRIPT_DIR/install.sh"

archive_name="tau_1.2.3_linux_amd64.tar.gz"
archive_path="$unit_tmp/$archive_name"
echo "fake archive contents" >"$archive_path"

good_sum=$(sha256_of "$archive_path")

good_checksums="$unit_tmp/checksums-good.txt"
printf '%s  %s\n' "$good_sum" "$archive_name" >"$good_checksums"

assert_success "valid checksum is accepted" \
  verify_checksum "$archive_path" "$archive_name" "$good_checksums"

missing_checksums="$unit_tmp/checksums-missing.txt"
printf '%s  %s\n' "$good_sum" "some_other_file.tar.gz" >"$missing_checksums"

assert_failure "missing checksum entry is rejected" \
  verify_checksum "$archive_path" "$archive_name" "$missing_checksums"

tampered_path="$unit_tmp/tampered.tar.gz"
echo "tampered contents" >"$tampered_path"

assert_failure "tampered archive is rejected (mismatch)" \
  verify_checksum "$tampered_path" "$archive_name" "$good_checksums"

assert_failure "missing checksums.txt file is rejected" \
  verify_checksum "$archive_path" "$archive_name" "$unit_tmp/does-not-exist.txt"

# ---------- Part 2: end-to-end run_install against a local file server ----------

e2e_root=$(mktemp -d)
trap 'rm -rf "$unit_tmp" "$e2e_root"' EXIT

fixtures="$e2e_root/fixtures"
version="v9.9.9"
plain_version="${version#v}"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) arch=amd64 ;;
esac
archive_name="tau_${plain_version}_${os}_${arch}.tar.gz"

build_release_fixture() {
  local dir="$1" binary_content="$2"
  mkdir -p "$dir/download/$version"
  local work
  work=$(mktemp -d)
  printf '%s' "$binary_content" >"$work/tau"
  chmod +x "$work/tau"
  tar -czf "$dir/download/$version/$archive_name" -C "$work" tau
  rm -rf "$work"
  sha256_of "$dir/download/$version/$archive_name"
}

mkdir -p "$fixtures"
sum=$(build_release_fixture "$fixtures" "real release binary")
printf '%s  %s\n' "$sum" "$archive_name" >"$fixtures/download/$version/checksums.txt"

port=$((20000 + RANDOM % 20000))
(cd "$fixtures" && exec python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1) &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null; rm -rf "$unit_tmp" "$e2e_root"' EXIT

for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$port/download/$version/checksums.txt" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

run_case_install_dir="$e2e_root/install-dir"

run_install_case() {
  mkdir -p "$run_case_install_dir"
  TAU_INSTALL_TEST_MODE=0 \
    TAU_BASE_URL="http://127.0.0.1:$port" \
    TAU_VERSION="$version" \
    TAU_INSTALL_DIR="$run_case_install_dir" \
    bash "$SCRIPT_DIR/install.sh" >/dev/null 2>&1
}

# Case A: valid archive + matching checksum installs cleanly.
rm -rf "$run_case_install_dir"
if run_install_case && [ -x "$run_case_install_dir/tau" ]; then
  ok "valid release installs cleanly end to end"
else
  not_ok "valid release installs cleanly end to end"
fi

# Case B: tampered archive is rejected; any pre-existing binary is preserved.
rm -rf "$run_case_install_dir"
mkdir -p "$run_case_install_dir"
printf '%s' "existing binary — must not be touched" >"$run_case_install_dir/tau"
chmod +x "$run_case_install_dir/tau"
before_hash=$(sha256_of "$run_case_install_dir/tau")

# Serve a tampered archive under a second version so the good fixture at
# $version is untouched, then point the install at it.
tampered_version="v9.9.8"
mkdir -p "$fixtures/download/$tampered_version"
printf '%s' "tampered payload" >"$fixtures/download/$tampered_version/$archive_name"
# checksums.txt still lists the *original* (correct) digest, simulating a
# corrupted-in-transit or tampered archive that no longer matches it.
printf '%s  %s\n' "$sum" "$archive_name" >"$fixtures/download/$tampered_version/checksums.txt"

set +e
TAU_INSTALL_TEST_MODE=0 \
  TAU_BASE_URL="http://127.0.0.1:$port" \
  TAU_VERSION="$tampered_version" \
  TAU_INSTALL_DIR="$run_case_install_dir" \
  bash "$SCRIPT_DIR/install.sh" >/dev/null 2>&1
tampered_exit=$?
set -e

after_hash=$(sha256_of "$run_case_install_dir/tau")

if [ "$tampered_exit" -ne 0 ]; then
  ok "tampered archive install exits nonzero"
else
  not_ok "tampered archive install exits nonzero"
fi

if [ "$before_hash" = "$after_hash" ]; then
  ok "existing binary is preserved after a tampered-archive install attempt"
else
  not_ok "existing binary is preserved after a tampered-archive install attempt"
fi

# Case C: missing checksum entry is rejected; existing binary preserved.
missing_version="v9.9.7"
mkdir -p "$fixtures/download/$missing_version"
cp "$fixtures/download/$version/$archive_name" "$fixtures/download/$missing_version/$archive_name"
printf '%s  %s\n' "$sum" "some_other_file.tar.gz" >"$fixtures/download/$missing_version/checksums.txt"

before_hash=$(sha256_of "$run_case_install_dir/tau")
set +e
TAU_INSTALL_TEST_MODE=0 \
  TAU_BASE_URL="http://127.0.0.1:$port" \
  TAU_VERSION="$missing_version" \
  TAU_INSTALL_DIR="$run_case_install_dir" \
  bash "$SCRIPT_DIR/install.sh" >/dev/null 2>&1
missing_exit=$?
set -e
after_hash=$(sha256_of "$run_case_install_dir/tau")

if [ "$missing_exit" -ne 0 ]; then
  ok "missing checksum entry install exits nonzero"
else
  not_ok "missing checksum entry install exits nonzero"
fi

if [ "$before_hash" = "$after_hash" ]; then
  ok "existing binary is preserved after a missing-checksum-entry install attempt"
else
  not_ok "existing binary is preserved after a missing-checksum-entry install attempt"
fi

kill "$server_pid" 2>/dev/null || true

echo ""
echo "$pass passed, $fail failed"
if [ "$fail" -ne 0 ]; then
  exit 1
fi
