#!/usr/bin/env bash
 
set -euo pipefail
 
# curl -fsSL https://raw.githubusercontent.com/BalanceBalls/nekot/main/install.sh | sh
 
text_bold() {
  echo -e "\033[1m$1\033[0m"
}
text_title() {
  echo ""
  text_bold "$1"
  if [ "$2" != "" ]; then echo "$2"; fi
}
text_title_error() {
    echo ""
    echo -e "\033[1;31m$1\033[00m"
}
 
NAME="nekot"
VERSION="latest"
GITHUB_REPO="BalanceBalls/nekot"
DOWNLOAD_BASE_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION"
LATEST_RELEASE_URL="https://github.com/$GITHUB_REPO/releases/latest"

TAG=$(curl -L -v $LATEST_RELEASE_URL 2>&1 | \
	grep 'GET /BalanceBalls/nekot/releases/tag' 2>&1 | \
	awk -F'v' '{print $2}' | cut -d' ' -f1) 

if [ "$VERSION" == "latest" ]; then
  DOWNLOAD_BASE_URL="https://github.com/$GITHUB_REPO/releases/download/v$TAG"
fi

PREFIX="nekot_${TAG}"
FILE_EXT="tar.gz"

OS="$(uname -s)"
ARCH="$(uname -m)"
SYSTEM="${OS}_${ARCH}"

case "${OS}_${ARCH}" in
  Linux_arm64)
    FILENAME="${PREFIX}_linux_arm64.${FILE_EXT}"
    ;;
  Linux_armel)
    FILENAME="${PREFIX}_linux_armv6.${FILE_EXT}"
    ;;
  Linux_armv6)
    FILENAME="${PREFIX}_linux_armv6.${FILE_EXT}"
    ;;
  Linux_armv7)
    FILENAME="${PREFIX}_linux_armv6.${FILE_EXT}"
    ;;
  Linux_x86_64)
    FILENAME="${PREFIX}_linux_amd64.${FILE_EXT}"
    ;;
  Darwin_x86_64)
    FILENAME="${PREFIX}_darwin_amd64.${FILE_EXT}"
    ;;
  Darwin_arm64)
    FILENAME="${PREFIX}_darwin_arm64.${FILE_EXT}"
    ;;
  *) 
    text_title_error "Error: Unsupported operating system or architecture."
    echo "Detected: ${OS}_${ARCH}"
    echo "Supported: Linux_x86_64, Linux_arm64, Linux_armv6, Linux_armv7, Linux_armel, Darwin_x86_64, Darwin_arm64" 
    exit 1
    ;;
esac
 
INSTALL_DIR="/usr/local/bin"
echo "$FILENAME"

DOWNLOAD_URL="$DOWNLOAD_BASE_URL/$FILENAME"
echo "$DOWNLOAD_URL"
 
TEMP_DIR=$(mktemp -d)
cd $TEMP_DIR
cleanup() { rm -rf $TEMP_DIR; }
trap cleanup EXIT

text_title "Downloading Binary" " $DOWNLOAD_URL"
curl --fail --show-error --location --progress-bar \
    $DOWNLOAD_URL | \
    tar -xzf - -C "."
 
text_title "Installing binary to" " $INSTALL_DIR/$NAME"
chmod +x "$NAME"
mv "$NAME" "$INSTALL_DIR/$NAME"
 
text_title "Installation complete" " Run $NAME --help for more information"
echo ""
