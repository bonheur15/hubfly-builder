#!/bin/bash

# hubfly-builder Installation & Update Script
# Usage: curl -sSL https://raw.githubusercontent.com/hubfly-space/hubfly-builder/main/scripts/install.sh | bash

set -e

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
REPO_URL="https://github.com/hubfly-space/hubfly-builder.git"

# 3. Prerequisites
check_dependencies() {
    echo -e "${YELLOW}Checking dependencies...${NC}"
    DEPS=("git" "go" "sqlite3" "buildctl")
    for dep in "${DEPS[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            echo -e "${RED}Error: $dep is not installed.${NC}"
            if [[ "$dep" == "buildctl" ]]; then
                echo -e "Please install BuildKit (moby/buildkit)."
            elif [[ "$dep" == "go" ]]; then
                echo -e "Please install Go (1.21+)."
            fi
            exit 1
        fi
    done
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

# 6. Build and Install
build_and_install() {
    echo -e "${YELLOW}Building hubfly-builder from source...${NC}"
    
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT
    
    cd "$TMP_DIR"
    git clone --depth 1 "$REPO_URL" .
    
    echo -e "${YELLOW}Running go build...${NC}"
    go build -o hubfly-builder ./cmd/hubfly-builder/main.go
    
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
    install -m 755 hubfly-builder "$INSTALL_DIR/hubfly-builder"
    
    # Install service file
    echo -e "${YELLOW}Installing systemd service...${NC}"
    cp packaging/systemd/hubfly-builder.service /etc/systemd/system/
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
setup_user
setup_dirs
build_and_install
finalize
