#!/usr/bin/env bash
# Tests for install.sh's prerequisite, checksum, archive, and
# temporary-binary validation (CAT-114, CAT-113).
#
# Usage: ./install_test.sh
#
# Part 1 sources install.sh in test mode (TAU_INSTALL_TEST_MODE=1) to unit
# test check_prerequisites/verify_checksum/sha256_of/extract_binary/
# verify_binary_runs directly, without touching the network. Part 2 runs
# the real run_install end to end against a local static file server
# standing in for GitHub releases, to prove that a failed verification
# leaves any existing installed binary untouched and exits nonzero, while
# a valid one installs cleanly.
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

# ---------- Part 1b: unit tests for check_prerequisites ----------

no_curl_dir="$unit_tmp/no-curl"
mkdir -p "$no_curl_dir"
cat >"$no_curl_dir/tar" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$no_curl_dir/tar"

assert_failure "missing curl is rejected" \
  bash -c "PATH='$no_curl_dir'; source '$SCRIPT_DIR/install.sh'; check_prerequisites"

no_tar_dir="$unit_tmp/no-tar"
mkdir -p "$no_tar_dir"
cat >"$no_tar_dir/curl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$no_tar_dir/curl"

assert_failure "missing tar is rejected" \
  bash -c "PATH='$no_tar_dir'; source '$SCRIPT_DIR/install.sh'; check_prerequisites"

have_both_dir="$unit_tmp/have-both"
mkdir -p "$have_both_dir"
for cmd in curl tar; do
  cat >"$have_both_dir/$cmd" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$have_both_dir/$cmd"
done

assert_success "curl and tar present is accepted" \
  bash -c "PATH='$have_both_dir'; source '$SCRIPT_DIR/install.sh'; check_prerequisites"

# ---------- Part 1c: unit tests for extract_binary / verify_binary_runs ----------

extract_tmp="$unit_tmp/extract-good"
mkdir -p "$extract_tmp"
good_bin_src="$unit_tmp/good-src"
mkdir -p "$good_bin_src"
printf '#!/usr/bin/env bash\nif [ "$1" = "--version" ]; then\n  echo "tau version 9.9.9 (fixture)"\n  exit 0\nfi\nexit 1\n' >"$good_bin_src/tau"
chmod +x "$good_bin_src/tau"
good_archive="$unit_tmp/good.tar.gz"
tar -czf "$good_archive" -C "$good_bin_src" tau

assert_success "extract_binary accepts an archive containing exactly 'tau'" \
  extract_binary "$good_archive" "$extract_tmp" tau

assert_success "verify_binary_runs accepts a binary that exits 0 on --version" \
  verify_binary_runs "$extract_tmp/tau"

symlink_bin_src="$unit_tmp/symlink-src"
mkdir -p "$symlink_bin_src"
printf '#!/usr/bin/env bash\necho "not the real tau"\n' >"$unit_tmp/symlink-target"
chmod +x "$unit_tmp/symlink-target"
ln -s "$unit_tmp/symlink-target" "$symlink_bin_src/tau"
symlink_archive="$unit_tmp/symlink.tar.gz"
tar -czf "$symlink_archive" -C "$symlink_bin_src" tau
symlink_extract_tmp="$unit_tmp/extract-symlink"
mkdir -p "$symlink_extract_tmp"

assert_failure "extract_binary rejects an archive where 'tau' is a symlink, not a regular file" \
  extract_binary "$symlink_archive" "$symlink_extract_tmp" tau

no_binary_src="$unit_tmp/no-binary-src"
mkdir -p "$no_binary_src"
printf 'not a binary' >"$no_binary_src/readme.txt"
no_binary_archive="$unit_tmp/no-binary.tar.gz"
tar -czf "$no_binary_archive" -C "$no_binary_src" readme.txt

assert_failure "extract_binary rejects an archive lacking 'tau'" \
  extract_binary "$no_binary_archive" "$unit_tmp/extract-missing" tau

bad_bin_src="$unit_tmp/bad-src"
mkdir -p "$bad_bin_src"
printf '#!/usr/bin/env bash\nexit 1\n' >"$bad_bin_src/tau"
chmod +x "$bad_bin_src/tau"
bad_extract_tmp="$unit_tmp/extract-bad"
mkdir -p "$bad_extract_tmp"
bad_archive="$unit_tmp/bad.tar.gz"
tar -czf "$bad_archive" -C "$bad_bin_src" tau

assert_success "extract_binary accepts an archive whose 'tau' fails --version (extraction only checks presence)" \
  extract_binary "$bad_archive" "$bad_extract_tmp" tau

assert_failure "verify_binary_runs rejects a binary that exits nonzero on --version" \
  verify_binary_runs "$bad_extract_tmp/tau"

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
good_release_binary=$'#!/usr/bin/env bash\nif [ "$1" = "--version" ]; then\n  echo "tau version 9.9.9 (fixture)"\n  exit 0\nfi\nexit 1\n'
sum=$(build_release_fixture "$fixtures" "$good_release_binary")
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
printf '%s' "existing binary - must not be touched" >"$run_case_install_dir/tau"
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

# Case D: archive lacking the tau binary is rejected; existing binary preserved.
no_binary_version="v9.9.6"
mkdir -p "$fixtures/download/$no_binary_version"
no_binary_work=$(mktemp -d)
printf 'not a binary' >"$no_binary_work/readme.txt"
tar -czf "$fixtures/download/$no_binary_version/$archive_name" -C "$no_binary_work" readme.txt
rm -rf "$no_binary_work"
no_binary_sum=$(sha256_of "$fixtures/download/$no_binary_version/$archive_name")
printf '%s  %s\n' "$no_binary_sum" "$archive_name" >"$fixtures/download/$no_binary_version/checksums.txt"

before_hash=$(sha256_of "$run_case_install_dir/tau")
set +e
TAU_INSTALL_TEST_MODE=0 \
  TAU_BASE_URL="http://127.0.0.1:$port" \
  TAU_VERSION="$no_binary_version" \
  TAU_INSTALL_DIR="$run_case_install_dir" \
  bash "$SCRIPT_DIR/install.sh" >/dev/null 2>&1
no_binary_exit=$?
set -e
after_hash=$(sha256_of "$run_case_install_dir/tau")

if [ "$no_binary_exit" -ne 0 ]; then
  ok "archive lacking tau binary install exits nonzero"
else
  not_ok "archive lacking tau binary install exits nonzero"
fi

if [ "$before_hash" = "$after_hash" ]; then
  ok "existing binary is preserved after an archive-lacking-tau-binary install attempt"
else
  not_ok "existing binary is preserved after an archive-lacking-tau-binary install attempt"
fi

# Case E: a temporary binary that fails --version is rejected; existing
# binary preserved.
bad_version_version="v9.9.5"
mkdir -p "$fixtures/download/$bad_version_version"
bad_version_work=$(mktemp -d)
printf '#!/usr/bin/env bash\nexit 1\n' >"$bad_version_work/tau"
chmod +x "$bad_version_work/tau"
tar -czf "$fixtures/download/$bad_version_version/$archive_name" -C "$bad_version_work" tau
rm -rf "$bad_version_work"
bad_version_sum=$(sha256_of "$fixtures/download/$bad_version_version/$archive_name")
printf '%s  %s\n' "$bad_version_sum" "$archive_name" >"$fixtures/download/$bad_version_version/checksums.txt"

before_hash=$(sha256_of "$run_case_install_dir/tau")
set +e
TAU_INSTALL_TEST_MODE=0 \
  TAU_BASE_URL="http://127.0.0.1:$port" \
  TAU_VERSION="$bad_version_version" \
  TAU_INSTALL_DIR="$run_case_install_dir" \
  bash "$SCRIPT_DIR/install.sh" >/dev/null 2>&1
bad_version_exit=$?
set -e
after_hash=$(sha256_of "$run_case_install_dir/tau")

if [ "$bad_version_exit" -ne 0 ]; then
  ok "temporary binary failing --version install exits nonzero"
else
  not_ok "temporary binary failing --version install exits nonzero"
fi

if [ "$before_hash" = "$after_hash" ]; then
  ok "existing binary is preserved after a failing-version-check install attempt"
else
  not_ok "existing binary is preserved after a failing-version-check install attempt"
fi

kill "$server_pid" 2>/dev/null || true

echo ""
echo "$pass passed, $fail failed"
if [ "$fail" -ne 0 ]; then
  exit 1
fi
