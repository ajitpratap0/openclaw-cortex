#!/usr/bin/env bash
# install.sh — download and install openclaw-cortex binary
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/ajitpratap0/openclaw-cortex/main/scripts/install.sh | bash
#
# Environment variables:
#   INSTALL_DIR    Installation directory (default: /usr/local/bin)
#   VERSION        Release version to install (default: latest)

set -euo pipefail

REPO="ajitpratap0/openclaw-cortex"
BINARY_NAME="openclaw-cortex"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

# --- helpers ---

info()  { printf '\033[0;32m[INFO]\033[0m  %s\n' "$*"; }
warn()  { printf '\033[0;33m[WARN]\033[0m  %s\n' "$*"; }
error() { printf '\033[0;31m[ERROR]\033[0m %s\n' "$*" >&2; exit 1; }

need_cmd() {
    if ! command -v "$1" &>/dev/null; then
        error "Required command not found: $1. Please install it and retry."
    fi
}

# --- detect OS and architecture ---

detect_os() {
    local os
    os="$(uname -s)"
    case "$os" in
        Linux)  echo "linux" ;;
        Darwin) echo "darwin" ;;
        *)      error "Unsupported operating system: $os. Only Linux and macOS are supported." ;;
    esac
}

detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64 | amd64)           echo "amd64" ;;
        arm64 | aarch64 | armv8*) echo "arm64" ;;
        *)                        error "Unsupported architecture: $arch. Only amd64 and arm64 are supported." ;;
    esac
}

# --- resolve latest version tag ---

resolve_version() {
    if [ "$VERSION" = "latest" ]; then
        need_cmd curl
        local api_url="https://api.github.com/repos/${REPO}/releases/latest"
        local tag
        tag="$(curl -fsSL "$api_url" | grep '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
        if [ -z "$tag" ]; then
            error "Could not determine latest release version from GitHub API. Try setting VERSION manually: VERSION=v1.0.0 bash install.sh"
        fi
        echo "$tag"
    else
        echo "$VERSION"
    fi
}

# --- download and install ---

main() {
    need_cmd curl
    need_cmd chmod

    local os arch version download_url tmp_file

    os="$(detect_os)"
    arch="$(detect_arch)"
    version="$(resolve_version)"

    info "Installing ${BINARY_NAME} ${version} (${os}/${arch})"

    download_url="https://github.com/${REPO}/releases/download/${version}/${BINARY_NAME}-${os}-${arch}"

    tmp_file="$(mktemp)"
    trap 'rm -f "$tmp_file"' EXIT

    info "Downloading from ${download_url}"
    if ! curl -fsSL -o "$tmp_file" "$download_url"; then
        error "Download failed. Check that release ${version} exists at https://github.com/${REPO}/releases"
    fi

    chmod +x "$tmp_file"

    # Verify the install directory exists and is writable; use sudo if needed.
    if [ ! -d "$INSTALL_DIR" ]; then
        warn "Install directory ${INSTALL_DIR} does not exist. Creating it."
        if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
            info "Creating ${INSTALL_DIR} requires sudo."
            sudo mkdir -p "$INSTALL_DIR"
        fi
    fi

    local dest="${INSTALL_DIR}/${BINARY_NAME}"
    if ! mv "$tmp_file" "$dest" 2>/dev/null; then
        info "Moving binary to ${dest} requires sudo."
        sudo mv "$tmp_file" "$dest"
        sudo chmod +x "$dest"
    fi

    info "Installed ${BINARY_NAME} to ${dest}"

    # Verify the binary runs.
    if ! "$dest" --version &>/dev/null; then
        warn "Binary installed but '${BINARY_NAME} --version' did not exit cleanly. This may be normal if dependencies are missing."
    fi

    # --- next steps ---

    printf '\n'
    info "Installation complete!"
    printf '\n'
    printf 'Next steps:\n'
    printf '\n'
    printf '  1. Start Qdrant (vector store):\n'
    printf '       docker compose up -d\n'
    printf '\n'
    printf '  2. Pull the embedding model:\n'
    printf '       ollama pull nomic-embed-text\n'
    printf '\n'
    printf '  3. Set your Anthropic API key (required for capture):\n'
    printf '       export ANTHROPIC_API_KEY=sk-ant-...\n'
    printf '\n'
    printf '  4. Explore commands:\n'
    printf '       %s --help\n' "$BINARY_NAME"
    printf '\n'
    printf 'Documentation: https://ajitpratap0.github.io/openclaw-cortex/\n'
    printf '\n'
}

main "$@"
