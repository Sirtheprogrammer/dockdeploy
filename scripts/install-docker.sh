#!/usr/bin/env bash
# ==============================================================================
# Official Docker & Docker Compose Installation Script
# Supports: Ubuntu, Debian, Raspberry Pi OS, CentOS, RHEL, Fedora, Rocky, Alma
# ==============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

TARGET_USER="${SUDO_USER:-$(id -un)}"
METHOD="convenience" # "convenience" or "apt-repo"
CHANNEL="stable"
CHECK_ONLY=false

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Installs the official Docker Engine and Docker Compose plugin on a Linux server.

Options:
  -u, --user <username>   User to add to the 'docker' group (default: ${TARGET_USER})
  -m, --method <name>     Installation method: 'convenience' (default) or 'apt-repo'
  -c, --channel <name>    Docker channel: 'stable' (default) or 'test'
  --check                 Check current Docker installation status without modifying
  -h, --help              Show this help message

Examples:
  sudo bash install-docker.sh
  sudo bash install-docker.sh --user deploy --method convenience
  bash install-docker.sh --check
EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        -u|--user)
            TARGET_USER="$2"
            shift 2
            ;;
        -m|--method)
            METHOD="$2"
            shift 2
            ;;
        -c|--channel)
            CHANNEL="$2"
            shift 2
            ;;
        --check)
            CHECK_ONLY=true
            shift
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}" >&2
            usage
            ;;
    esac
done

check_docker() {
    echo -e "${BLUE}==> Checking Docker installation status...${NC}"
    local installed=true
    if command -v docker >/dev/null 2>&1; then
        echo -e "  ${GREEN}✓${NC} Docker CLI: $(docker --version)"
    else
        echo -e "  ${YELLOW}✗${NC} Docker CLI: Not installed"
        installed=false
    fi

    if docker compose version >/dev/null 2>&1; then
        echo -e "  ${GREEN}✓${NC} Docker Compose: $(docker compose version)"
    elif command -v docker-compose >/dev/null 2>&1; then
        echo -e "  ${GREEN}✓${NC} Docker Compose (legacy): $(docker-compose --version)"
    else
        echo -e "  ${YELLOW}✗${NC} Docker Compose: Not installed"
    fi

    if command -v systemctl >/dev/null 2>&1; then
        if systemctl is-active --quiet docker 2>/dev/null; then
            echo -e "  ${GREEN}✓${NC} Docker Daemon: Active & running"
        else
            echo -e "  ${YELLOW}✗${NC} Docker Daemon: Inactive or not running"
            installed=false
        fi
    fi

    if [[ "$installed" = true ]]; then
        echo -e "${GREEN}Docker is fully installed and active!${NC}"
        return 0
    else
        echo -e "${YELLOW}Docker is not fully installed or active.${NC}"
        return 1
    fi
}

if [[ "$CHECK_ONLY" = true ]]; then
    check_docker
    exit $?
fi

# Privilege verification
ELEVATE=""
if [[ $EUID -ne 0 ]]; then
    if command -v sudo >/dev/null 2>&1; then
        ELEVATE="sudo"
        echo -e "${BLUE}[info]${NC} Running with sudo elevation"
    else
        echo -e "${RED}[error] This script must be run as root or with sudo installed.${NC}" >&2
        exit 1
    fi
fi

echo -e "${BLUE}======================================================${NC}"
echo -e "${BLUE}   Official Docker Installation & Configuration      ${NC}"
echo -e "${BLUE}======================================================${NC}"
echo -e "Target user: ${YELLOW}${TARGET_USER}${NC}"
echo -e "Method:      ${YELLOW}${METHOD}${NC}"
echo -e "Channel:     ${YELLOW}${CHANNEL}${NC}"
echo ""

# Ensure curl / wget & basic dependencies are present
echo -e "${BLUE}==> Installing required system packages (curl, ca-certificates)...${NC}"
if command -v apt-get >/dev/null 2>&1; then
    $ELEVATE apt-get update -qq
    $ELEVATE apt-get install -y -qq curl ca-certificates gnupg lsb-release >/dev/null
elif command -v dnf >/dev/null 2>&1; then
    $ELEVATE dnf install -y -q curl ca-certificates
elif command -v yum >/dev/null 2>&1; then
    $ELEVATE yum install -y -q curl ca-certificates
fi

if [[ "$METHOD" == "apt-repo" ]] && command -v apt-get >/dev/null 2>&1; then
    echo -e "${BLUE}==> Setting up official Docker APT repository...${NC}"
    $ELEVATE install -m 0755 -d /etc/apt/keyrings
    
    # Detect distro (ubuntu / debian / raspbian)
    DISTRO="ubuntu"
    if [[ -f /etc/os-release ]]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        DISTRO="${ID:-ubuntu}"
    fi

    if [[ "$DISTRO" != "ubuntu" && "$DISTRO" != "debian" && "$DISTRO" != "raspbian" ]]; then
        echo -e "${YELLOW}Distro '$DISTRO' not natively handled by apt-repo branch, falling back to official convenience script.${NC}"
        METHOD="convenience"
    else
        GPG_URL="https://download.docker.com/linux/${DISTRO}/gpg"
        echo -e "Downloading GPG key from ${GPG_URL}..."
        curl -fsSL "$GPG_URL" | $ELEVATE gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
        $ELEVATE chmod a+r /etc/apt/keyrings/docker.gpg

        VERSION_CODENAME="${VERSION_CODENAME:-}"
        if [[ -z "$VERSION_CODENAME" && -f /etc/os-release ]]; then
            # shellcheck disable=SC1091
            . /etc/os-release
            VERSION_CODENAME="${VERSION_CODENAME:-}"
        fi

        ARCH="$(dpkg --print-architecture)"
        echo \
          "deb [arch=${ARCH} signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/${DISTRO} ${VERSION_CODENAME} ${CHANNEL}" | \
          $ELEVATE tee /etc/apt/sources.list.d/docker.list > /dev/null

        $ELEVATE apt-get update -qq
        echo -e "${BLUE}==> Installing Docker Engine, containerd, and Compose plugin...${NC}"
        $ELEVATE apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    fi
fi

if [[ "$METHOD" == "convenience" ]]; then
    echo -e "${BLUE}==> Executing official Docker convenience script (https://get.docker.com)...${NC}"
    # Download script to a secure temporary file
    TMP_SCRIPT="$(mktemp /tmp/get-docker.XXXXXX.sh)"
    curl -fsSL https://get.docker.com -o "$TMP_SCRIPT"
    
    # Execute installer with optional channel
    if [[ "$CHANNEL" != "stable" ]]; then
        CHANNEL="$CHANNEL" $ELEVATE sh "$TMP_SCRIPT"
    else
        $ELEVATE sh "$TMP_SCRIPT"
    fi
    rm -f "$TMP_SCRIPT"
fi

# Enable and start systemd services
echo -e "${BLUE}==> Enabling and starting Docker daemon service...${NC}"
if command -v systemctl >/dev/null 2>&1; then
    $ELEVATE systemctl enable docker.service >/dev/null 2>&1 || true
    $ELEVATE systemctl enable containerd.service >/dev/null 2>&1 || true
    $ELEVATE systemctl start docker.service || true
fi

# Add user to docker group
if [[ -n "$TARGET_USER" ]] && id "$TARGET_USER" >/dev/null 2>&1; then
    echo -e "${BLUE}==> Adding user '${TARGET_USER}' to the 'docker' group...${NC}"
    $ELEVATE groupadd -f docker
    $ELEVATE usermod -aG docker "$TARGET_USER"
    echo -e "${GREEN}✓ User '${TARGET_USER}' added to group 'docker'${NC}"
fi

# Verification
echo ""
echo -e "${GREEN}======================================================${NC}"
echo -e "${GREEN}      Docker Installation Completed Successfully!     ${NC}"
echo -e "${GREEN}======================================================${NC}"
if command -v docker >/dev/null 2>&1; then
    docker --version || true
fi
if docker compose version >/dev/null 2>&1; then
    docker compose version || true
fi

echo ""
echo -e "${YELLOW}Notice:${NC} If you are running commands as user '${TARGET_USER}', apply the group membership with:"
echo -e "        ${BLUE}newgrp docker${NC}   (or log out and back in via SSH)"
echo ""
