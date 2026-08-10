#!/bin/bash

set -euo pipefail
umask 077

IDENTITY_NAME="SoloQueue Code Signing"
BACKUP_BASENAME="SoloQueue-Code-Signing"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"
PIN_PATH="$REPO_ROOT/desktop/build/macos-signing-cert.sha256"
IMPORT_HELPER_SOURCE="$SCRIPT_DIR/macos-keychain-import.c"
BACKUP_DIR="$HOME/Documents/SoloQueue Signing Backup"
DEFAULT_BACKUP_PATH="$BACKUP_DIR/$BACKUP_BASENAME.p12"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "Usage: $0 --create"
  echo "       $0 --restore /absolute/path/to/$BACKUP_BASENAME.p12"
  echo "       $0 --check"
}

require_macos() {
  [[ "$(uname -s)" == "Darwin" ]] || fail "macOS is required"
  for command in /usr/bin/openssl /usr/bin/security /usr/bin/codesign /usr/bin/osascript /usr/bin/xcrun; do
    [[ -x "$command" ]] || fail "Required command not found: $command"
  done
  [[ -f "$IMPORT_HELPER_SOURCE" && ! -L "$IMPORT_HELPER_SOURCE" ]] \
    || fail "Keychain import helper source not found: $IMPORT_HELPER_SOURCE"
}

default_keychain() {
  local keychain
  keychain="$(/usr/bin/security default-keychain -d user \
    | /usr/bin/sed -E 's/^[[:space:]]*"//; s/"[[:space:]]*$//')"
  [[ -n "$keychain" && -f "$keychain" ]] || fail "Unable to resolve the user default Keychain"
  printf '%s\n' "$keychain"
}

prompt_secret() {
  local message="$1"
  /usr/bin/osascript - "$message" <<'APPLESCRIPT'
on run argv
  set promptMessage to item 1 of argv
  set response to display dialog promptMessage default answer "" with hidden answer buttons {"Cancel", "Continue"} default button "Continue" with title "SoloQueue Code Signing"
  return text returned of response
end run
APPLESCRIPT
}

normalize_fingerprint() {
  /usr/bin/tr -d ':[:space:]' | /usr/bin/tr '[:upper:]' '[:lower:]'
}

certificate_fingerprint() {
  local certificate_path="$1"
  /usr/bin/openssl x509 -in "$certificate_path" -noout -fingerprint -sha256 \
    | /usr/bin/awk -F= 'NF == 2 { print $2 }' \
    | normalize_fingerprint
}

read_pinned_fingerprint() {
  [[ -f "$PIN_PATH" && ! -L "$PIN_PATH" ]] || fail "Pinned fingerprint not found: $PIN_PATH"
  local fingerprint
  fingerprint="$(normalize_fingerprint < "$PIN_PATH")"
  [[ "$fingerprint" =~ ^[0-9a-f]{64}$ ]] || fail "Pinned fingerprint is invalid: $PIN_PATH"
  printf '%s\n' "$fingerprint"
}

validate_certificate() {
  local certificate_path="$1"
  local subject details
  subject="$(/usr/bin/openssl x509 -in "$certificate_path" -noout -subject -nameopt RFC2253)"
  [[ "$subject" == *"CN=$IDENTITY_NAME"* ]] || fail "Certificate common name is not $IDENTITY_NAME"
  details="$(/usr/bin/openssl x509 -in "$certificate_path" -noout -text)"
  [[ "$details" == *"Code Signing"* ]] || fail "Certificate is not valid for code signing"
}

keychain_certificate_fingerprint() {
  local keychain="$1"
  local temp_dir certificate_path
  temp_dir="$(mktemp -d /tmp/soloqueue-keychain-cert.XXXXXX)"
  certificate_path="$temp_dir/certificate.pem"
  /usr/bin/security find-certificate -c "$IDENTITY_NAME" -p "$keychain" > "$certificate_path"
  validate_certificate "$certificate_path"
  certificate_fingerprint "$certificate_path"
  rm -f "$certificate_path"
  rmdir "$temp_dir"
}

matching_identity_count() {
  local keychain="$1"
  /usr/bin/security find-identity -v -p codesigning "$keychain" \
    | /usr/bin/awk -v name="$IDENTITY_NAME" 'index($0, "\"" name "\"") { count++ } END { print count + 0 }'
}

verify_installed_identity() {
  local keychain="$1"
  local expected_fingerprint="$2"
  local count actual_fingerprint
  count="$(matching_identity_count "$keychain")"
  [[ "$count" == "1" ]] || fail "Expected exactly one valid $IDENTITY_NAME identity, found $count"
  actual_fingerprint="$(keychain_certificate_fingerprint "$keychain")"
  [[ "$actual_fingerprint" == "$expected_fingerprint" ]] \
    || fail "Installed identity fingerprint does not match the pinned certificate"
  echo "Verified identity: $IDENTITY_NAME"
  echo "Certificate SHA-256: $actual_fingerprint"
}

import_identity() {
  local keychain="$1"
  local pkcs12_path="$2"
  local certificate_path="$3"
  local password="$4"
  local temp_dir="$5"
  local clang_path helper_path
  clang_path="$(/usr/bin/xcrun --find clang)"
  [[ -x "$clang_path" ]] || fail "Apple clang is required to build the Keychain import helper"
  helper_path="$temp_dir/macos-keychain-import"

  "$clang_path" \
    -std=c11 \
    -Wall \
    -Wextra \
    -Werror \
    -Wno-deprecated-declarations \
    -framework CoreFoundation \
    -framework Security \
    "$IMPORT_HELPER_SOURCE" \
    -o "$helper_path"

  # Keep the decrypted private key in memory pipes only. The helper grants access only to codesign.
  /usr/bin/openssl pkcs12 \
    -in "$pkcs12_path" \
    -nocerts \
    -nodes \
    -passin fd:3 \
    3<<<"$password" \
    | /usr/bin/openssl rsa \
    | "$helper_path" "$keychain"
  /usr/bin/security add-trusted-cert -r trustRoot -p codeSign -k "$keychain" "$certificate_path"
}

create_identity() {
  local keychain="$1"
  [[ ! -e "$PIN_PATH" ]] || fail "Pinned fingerprint already exists; restore the existing identity instead"
  [[ "$(matching_identity_count "$keychain")" == "0" ]] \
    || fail "$IDENTITY_NAME already exists; refusing to replace it"
  [[ ! -e "$DEFAULT_BACKUP_PATH" ]] || fail "Backup already exists: $DEFAULT_BACKUP_PATH"

  local password confirmation temp_dir private_key certificate pkcs12 fingerprint
  password="$(prompt_secret "Create a password of at least 12 characters for the encrypted signing backup.")"
  [[ ${#password} -ge 12 ]] || fail "The backup password must contain at least 12 characters"
  confirmation="$(prompt_secret "Enter the same backup password again.")"
  [[ "$password" == "$confirmation" ]] || fail "The backup passwords do not match"
  unset confirmation

  temp_dir="$(mktemp -d /tmp/soloqueue-signing-create.XXXXXX)"
  private_key="$temp_dir/private-key.pem"
  certificate="$temp_dir/certificate.pem"
  pkcs12="$temp_dir/$BACKUP_BASENAME.p12"
  trap 'rm -f "$private_key" "$certificate" "$pkcs12"; rmdir "$temp_dir" 2>/dev/null || true' EXIT INT TERM

  /usr/bin/openssl req \
    -new \
    -newkey rsa:3072 \
    -x509 \
    -sha256 \
    -days 3650 \
    -subj "/CN=$IDENTITY_NAME/O=SoloQueue" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,digitalSignature,keyCertSign" \
    -addext "extendedKeyUsage=critical,codeSigning" \
    -keyout "$private_key" \
    -out "$certificate" \
    -passout fd:3 \
    3<<<"$password"

  /usr/bin/openssl pkcs12 \
    -export \
    -inkey "$private_key" \
    -in "$certificate" \
    -name "$IDENTITY_NAME" \
    -keypbe AES-256-CBC \
    -certpbe AES-256-CBC \
    -macalg sha256 \
    -out "$pkcs12" \
    -passin fd:3 \
    -passout fd:4 \
    3<<<"$password" \
    4<<<"$password"
  validate_certificate "$certificate"
  fingerprint="$(certificate_fingerprint "$certificate")"
  [[ "$fingerprint" =~ ^[0-9a-f]{64}$ ]] || fail "Generated certificate fingerprint is invalid"

  mkdir -p "$BACKUP_DIR"
  chmod 0700 "$BACKUP_DIR"
  mv "$pkcs12" "$DEFAULT_BACKUP_PATH"
  chmod 0600 "$DEFAULT_BACKUP_PATH"
  /usr/bin/openssl x509 -in "$certificate" -outform DER -out "$BACKUP_DIR/$BACKUP_BASENAME.cer"
  chmod 0644 "$BACKUP_DIR/$BACKUP_BASENAME.cer"
  printf '%s\n' "$fingerprint" > "$BACKUP_DIR/$BACKUP_BASENAME.sha256"
  chmod 0644 "$BACKUP_DIR/$BACKUP_BASENAME.sha256"

  import_identity "$keychain" "$DEFAULT_BACKUP_PATH" "$certificate" "$password" "$temp_dir"
  unset password
  verify_installed_identity "$keychain" "$fingerprint"

  echo "Encrypted backup: $DEFAULT_BACKUP_PATH"
  echo "Fingerprint file: $BACKUP_DIR/$BACKUP_BASENAME.sha256"
  echo "Repository pin required: $PIN_PATH"
  echo "Store the backup password in a separate password manager."
}

restore_identity() {
  local keychain="$1"
  local backup_path="$2"
  [[ "$backup_path" == /* ]] || fail "Restore path must be absolute"
  [[ "$backup_path" == *.p12 ]] || fail "Restore file must use the .p12 extension"
  [[ -f "$backup_path" && ! -L "$backup_path" && -r "$backup_path" ]] \
    || fail "Restore file must be a readable regular file: $backup_path"

  local expected_fingerprint password temp_dir certificate actual_fingerprint
  expected_fingerprint="$(read_pinned_fingerprint)"
  if [[ "$(matching_identity_count "$keychain")" != "0" ]]; then
    verify_installed_identity "$keychain" "$expected_fingerprint"
    echo "The fixed identity is already installed; no import was needed."
    return
  fi

  password="$(prompt_secret "Enter the password for the encrypted SoloQueue signing backup.")"
  [[ -n "$password" ]] || fail "The backup password cannot be empty"
  temp_dir="$(mktemp -d /tmp/soloqueue-signing-restore.XXXXXX)"
  certificate="$temp_dir/certificate.pem"
  trap 'rm -f "$certificate"; rmdir "$temp_dir" 2>/dev/null || true' EXIT INT TERM

  /usr/bin/openssl pkcs12 \
    -in "$backup_path" \
    -clcerts \
    -nokeys \
    -out "$certificate" \
    -passin fd:3 \
    3<<<"$password"
  validate_certificate "$certificate"
  actual_fingerprint="$(certificate_fingerprint "$certificate")"
  [[ "$actual_fingerprint" == "$expected_fingerprint" ]] \
    || fail "Backup certificate fingerprint does not match the repository pin"

  import_identity "$keychain" "$backup_path" "$certificate" "$password" "$temp_dir"
  unset password
  verify_installed_identity "$keychain" "$expected_fingerprint"
  echo "Restored $IDENTITY_NAME from $backup_path"
}

main() {
  require_macos
  local mode="${1:-}"
  local keychain expected_fingerprint
  keychain="$(default_keychain)"

  case "$mode" in
    --create)
      [[ $# -eq 1 ]] || { usage; exit 2; }
      create_identity "$keychain"
      ;;
    --restore)
      [[ $# -eq 2 ]] || { usage; exit 2; }
      restore_identity "$keychain" "$2"
      ;;
    --check)
      [[ $# -eq 1 ]] || { usage; exit 2; }
      expected_fingerprint="$(read_pinned_fingerprint)"
      verify_installed_identity "$keychain" "$expected_fingerprint"
      ;;
    *)
      usage
      exit 2
      ;;
  esac
}

main "$@"
