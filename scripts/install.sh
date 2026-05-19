#!/bin/bash

# hubfly-builder Installation & Update Script
# Usage: curl -sSL https://raw.githubusercontent.com/hubfly-space/hubfly-builder/main/scripts/install.sh | bash

set -euo pipefail

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}==> Hubfly Builder Installer/Updater${NC}"

# 1. Check for root
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}Error: This script must be run as root (use sudo)${NC}"
   exit 1
fi

# 2. Configuration
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/hubfly-builder"
DATA_DIR="/var/lib/hubfly-builder"
LOG_DIR="/var/log/hubfly-builder"
USER="hubfly-builder"
SERVICE_NAME="hubfly-builder"
UPDATE_LOCKFILE="/run/hubfly-builder-update.lock"
REPO_OWNER="hubfly-space"
REPO_NAME="hubfly-builder"
RAW_BASE_URL="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}"
RELEASE_BASE_URL="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download"
LATEST_RELEASE_API="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
INSTALL_VERSION="${INSTALL_VERSION:-${1:-latest}}"

ARCH=""
VERSION_TAG=""
TMP_DIR=""

# 3. Prerequisites
check_dependencies() {
    echo -e "${YELLOW}Checking dependencies...${NC}"
    DEPS=("curl" "tar" "hubcell" "systemctl" "visudo")
    for dep in "${DEPS[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            echo -e "${RED}Error: $dep is not installed.${NC}"
            if [[ "$dep" == "hubcell" ]]; then
                echo -e "Please install Hubcell CLI."
            fi
            exit 1
        fi
    done
}

detect_platform() {
    if [[ "$(uname -s)" != "Linux" ]]; then
        echo -e "${RED}Error: this installer currently supports Linux only.${NC}"
        exit 1
    fi

    case "$(uname -m)" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            echo -e "${RED}Error: unsupported architecture $(uname -m).${NC}"
            exit 1
            ;;
    esac
}

resolve_version() {
    if [[ "$INSTALL_VERSION" == "latest" ]]; then
        echo -e "${YELLOW}Resolving latest release version...${NC}"
        VERSION_TAG="$(curl -fsSL "$LATEST_RELEASE_API" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
        if [[ -z "$VERSION_TAG" ]]; then
            echo -e "${RED}Error: could not resolve the latest release version.${NC}"
            exit 1
        fi
    else
        VERSION_TAG="$INSTALL_VERSION"
    fi

    if [[ "$VERSION_TAG" != v* ]]; then
        VERSION_TAG="v${VERSION_TAG}"
    fi

    echo -e "${GREEN}Using release ${VERSION_TAG}${NC}"
}

verify_checksum() {
    local checksum_file="$1"
    local asset_file="$2"

    if command -v sha256sum >/dev/null 2>&1; then
        (cd "$(dirname "$asset_file")" && sha256sum -c "$(basename "$checksum_file")")
        return
    fi

    if command -v shasum >/dev/null 2>&1; then
        local expected actual
        expected="$(awk '{print $1}' "$checksum_file")"
        actual="$(shasum -a 256 "$asset_file" | awk '{print $1}')"
        if [[ "$expected" != "$actual" ]]; then
            echo -e "${RED}Error: checksum verification failed for $(basename "$asset_file").${NC}"
            exit 1
        fi
        return
    fi

    echo -e "${RED}Error: neither sha256sum nor shasum is available for checksum verification.${NC}"
    exit 1
}

# 4. User creation
setup_user() {
    if ! id "$USER" &>/dev/null; then
        echo -e "${YELLOW}Creating user $USER...${NC}"
        useradd --system --shell /usr/sbin/nologin --home-dir "$DATA_DIR" "$USER"
    fi
}

# 5. Directory setup
setup_dirs() {
    echo -e "${YELLOW}Setting up directories...${NC}"
    mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
    chown -R "$USER":"$USER" "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
    chmod 750 "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
}

# 6. Download and Install
download_and_install() {
    local asset_name="hubfly-builder_linux_${ARCH}.tar.gz"
    local asset_url="${RELEASE_BASE_URL}/${VERSION_TAG}/${asset_name}"
    local checksum_url="${asset_url}.sha256"
    local ref_url="${RAW_BASE_URL}/${VERSION_TAG}"

    echo -e "${YELLOW}Downloading release bundle ${asset_name}...${NC}"

    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    curl -fsSL "$asset_url" -o "$TMP_DIR/${asset_name}"
    curl -fsSL "$checksum_url" -o "$TMP_DIR/${asset_name}.sha256"
    verify_checksum "$TMP_DIR/${asset_name}.sha256" "$TMP_DIR/${asset_name}"

    tar -xzf "$TMP_DIR/${asset_name}" -C "$TMP_DIR"
    if [[ ! -f "$TMP_DIR/hubfly-builder" ]]; then
        echo -e "${RED}Error: release archive did not contain hubfly-builder.${NC}"
        exit 1
    fi

    echo -e "${YELLOW}Downloading systemd and sudoers files for ${VERSION_TAG}...${NC}"
    curl -fsSL "${ref_url}/packaging/systemd/hubfly-builder.service" -o "$TMP_DIR/hubfly-builder.service"
    curl -fsSL "${ref_url}/packaging/sudoers/hubfly-builder" -o "$TMP_DIR/hubfly-builder.sudoers"

    # 7. Check if update is safe (Lockfile check)
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        echo -e "${YELLOW}Service is running, checking for active builds...${NC}"
        while [ -f "$UPDATE_LOCKFILE" ]; do
            echo -e "${YELLOW}Build is currently in progress. Waiting for it to finish before updating...${NC}"
            sleep 10
        done
        echo -e "${GREEN}No active builds. Proceeding with update...${NC}"
        systemctl stop "$SERVICE_NAME"
    fi

    echo -e "${YELLOW}Installing binary...${NC}"
    install -m 755 "$TMP_DIR/hubfly-builder" "$INSTALL_DIR/hubfly-builder"

    # Install service file
    echo -e "${YELLOW}Installing systemd service...${NC}"
    install -m 644 "$TMP_DIR/hubfly-builder.service" /etc/systemd/system/hubfly-builder.service

    # Install sudoers file
    echo -e "${YELLOW}Installing sudoers file...${NC}"
    mkdir -p /etc/sudoers.d
    install -m 440 "$TMP_DIR/hubfly-builder.sudoers" /etc/sudoers.d/hubfly-builder
    visudo -cf /etc/sudoers.d/hubfly-builder || (echo -e "${RED}Error: sudoers file validation failed${NC}" && exit 1)

    systemctl daemon-reload
}

# 8. Finalize
finalize() {
    echo -e "${YELLOW}Starting $SERVICE_NAME...${NC}"
    systemctl enable "$SERVICE_NAME"
    systemctl start "$SERVICE_NAME"
    
    echo -e "${GREEN}==> Hubfly Builder successfully installed/updated!${NC}"
    echo -e "Status: $(systemctl is-active $SERVICE_NAME)"
    echo -e "Logs: journalctl -u $SERVICE_NAME -f"
}

# Execution
check_dependencies
detect_platform
resolve_version
setup_user
setup_dirs
download_and_install
finalize
