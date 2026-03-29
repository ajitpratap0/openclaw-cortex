#!/usr/bin/env bash
# download_eval_datasets.sh — Downloads LoCoMo, LongMemEval, and DMR datasets
# with SHA256 verification.
#
# Each file is downloaded to eval/<name>/testdata/<name>_dataset.json.
# Update the EXPECTED_SHA256_* variables below once real checksums are known.
#
# Usage:
#   bash scripts/download_eval_datasets.sh
#   bash scripts/download_eval_datasets.sh --skip-verify   # skip SHA256 check
#
# Requirements: curl (or wget), shasum (macOS) or sha256sum (Linux).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SKIP_VERIFY=false

for arg in "$@"; do
  case "$arg" in
    --skip-verify) SKIP_VERIFY=true ;;
    *) echo "Unknown argument: $arg" >&2; exit 1 ;;
  esac
done

# ---------------------------------------------------------------------------
# Dataset URLs — update when real URLs are available.
# ---------------------------------------------------------------------------
# TODO: Replace placeholder URLs with real dataset download URLs.

LOCOMO_URL=""
LOCOMO_DEST="${REPO_ROOT}/eval/locomo/testdata/locomo_dataset.json"
EXPECTED_SHA256_LOCOMO="TODO"  # replace with real checksum after first download

LONGMEMEVAL_URL=""
LONGMEMEVAL_DEST="${REPO_ROOT}/eval/longmemeval/testdata/longmemeval_dataset.json"
EXPECTED_SHA256_LONGMEMEVAL="TODO"  # replace with real checksum after first download

DMR_URL=""
DMR_DEST="${REPO_ROOT}/eval/dmr/testdata/dmr_dataset.json"
EXPECTED_SHA256_DMR="TODO"  # replace with real checksum after first download

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

sha256_file() {
  local file="$1"
  if command -v sha256sum &>/dev/null; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum &>/dev/null; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    echo "ERROR: neither sha256sum nor shasum found" >&2
    exit 1
  fi
}

download_file() {
  local url="$1"
  local dest="$2"
  local name="$3"

  if [[ -z "$url" ]]; then
    echo "[SKIP] $name: URL not configured — set ${name}_URL in this script to enable download."
    return 0
  fi

  mkdir -p "$(dirname "$dest")"

  echo "[DOWNLOAD] $name → $dest"
  if command -v curl &>/dev/null; then
    curl -fsSL --retry 3 -o "$dest" "$url"
  elif command -v wget &>/dev/null; then
    wget -q -O "$dest" "$url"
  else
    echo "ERROR: neither curl nor wget found" >&2
    exit 1
  fi
  echo "[OK] $name downloaded."
}

verify_checksum() {
  local file="$1"
  local expected="$2"
  local name="$3"

  if [[ "$expected" == "TODO" ]]; then
    local actual
    actual="$(sha256_file "$file")"
    echo "[WARN] $name: expected checksum not set. Actual SHA256: $actual"
    echo "       Update EXPECTED_SHA256_${name^^} in this script."
    return 0
  fi

  local actual
  actual="$(sha256_file "$file")"
  if [[ "$actual" != "$expected" ]]; then
    echo "ERROR: $name checksum mismatch!" >&2
    echo "  expected: $expected" >&2
    echo "  actual:   $actual" >&2
    exit 1
  fi
  echo "[OK] $name checksum verified."
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

echo "Downloading evaluation datasets..."
echo "REPO_ROOT: $REPO_ROOT"
echo ""

download_file "$LOCOMO_URL"      "$LOCOMO_DEST"      "locomo"
download_file "$LONGMEMEVAL_URL" "$LONGMEMEVAL_DEST" "longmemeval"
download_file "$DMR_URL"         "$DMR_DEST"         "dmr"

if [[ "$SKIP_VERIFY" == "true" ]]; then
  echo ""
  echo "[SKIP] Checksum verification skipped (--skip-verify)."
else
  echo ""
  echo "Verifying checksums..."
  [[ -f "$LOCOMO_DEST"      ]] && verify_checksum "$LOCOMO_DEST"      "$EXPECTED_SHA256_LOCOMO"      "locomo"
  [[ -f "$LONGMEMEVAL_DEST" ]] && verify_checksum "$LONGMEMEVAL_DEST" "$EXPECTED_SHA256_LONGMEMEVAL" "longmemeval"
  [[ -f "$DMR_DEST"         ]] && verify_checksum "$DMR_DEST"         "$EXPECTED_SHA256_DMR"         "dmr"
fi

echo ""
echo "Done. Run benchmarks with:"
echo "  go run ./eval/cmd/eval --benchmark locomo      --dataset-path ${LOCOMO_DEST}"
echo "  go run ./eval/cmd/eval --benchmark longmemeval --dataset-path ${LONGMEMEVAL_DEST}"
echo "  go run ./eval/cmd/eval --benchmark dmr         --dataset-path ${DMR_DEST}"
