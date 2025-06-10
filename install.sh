#!/usr/bin/env bash
 
set -euo pipefail
 
# curl -s https://raw.githubusercontent.com/BalanceBalls/nekot/main/install.sh | bash -s -- -u
 
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
 
if [ "$VERSION" == "latest" ]; then
  DOWNLOAD_BASE_URL="https://github.com/$GITHUB_REPO/releases/latest/download"
fi
 
TAG=$(curl -L -v $LATEST_RELEASE_URL 2>&1 | \
	grep 'GET /BalanceBalls/nekot/releases/tag' 2>&1 | \
	awk -F'v' '{print $2}' | cut -d' ' -f1) 

PREFIX="nekot_${TAG}"
FILE_EXT="tar.gz"

 #  ["Linux_arm64"]=$TAG + "linux.arm64"+$FILE_EXT
	# ["Linux_armel"]=$TAG + "linux.armv6" + $FILE_EXT
 #  ["Linux_armv6"]=$TAG + "linux.armv6" + $FILE_EXT
 #  ["Linux_armv7"]=$TAG + "linux.armv6" + $FILE_EXT
OS="$(uname -s)"
ARCH="$(uname -m)"
SYSTEM="${OS}_${ARCH}"

case "${OS}_${ARCH}" in
  Linux_x86_64)
    FILENAME="${PREFIX}_linux.amd64.${FILE_EXT}"
    ;;
  Darwin_x86_64) # macOS Intel
    FILENAME="${PREFIX}_darwin.amd64.${FILE_EXT}"
    ;;
  Darwin_arm64) # macOS Apple Silicon
    FILENAME="${PREFIX}_darwin.arm64.${FILE_EXT}"
    ;;
  *) 
    text_title_error "Error: Unsupported operating system or architecture."
    echo "Detected: ${OS}_${ARCH}"
    echo "Supported: Linux_x86_64, Darwin_x86_64, Darwin_arm64" # Add other supported platforms if any
    exit 1
    ;;
esac
 
INSTALL_DIR="/usr/local/bin"
echo "$FILENAME"

DOWNLOAD_URL="$DOWNLOAD_BASE_URL/$FILENAME"
echo "$DOWNLOAD_URL"
 
# if [ $# -gt 0 ]; then
#   while getopts ":ud:" opt; do
#   case $opt in
#     u)
#       # Set default install dir based on OS
#       [ "$OS" == "Darwin" ] && INSTALL_DIR="$HOME/bin" || INSTALL_DIR="$HOME/.local/bin"
#  
#       # Check that the user bin directory is in their PATH
#       IFS=':' read -ra PATHS <<< "$PATH"
#       INSTALL_DIR_IN_PATH="false"
#       for P in "${PATHS[@]}"; do
#         if [[ "$P" == "$INSTALL_DIR" ]]; then
#           INSTALL_DIR_IN_PATH="true"
#         fi
#       done
#  
#       # If user bin directory doesn't exist or not in PATH, exit
#       if [ ! -d "$INSTALL_DIR" ] || [ "$INSTALL_DIR_IN_PATH" == "false" ]; then
#         text_title_error "Error"
#         echo " The user bin directory '$INSTALL_DIR' does not exist or is not in your environment PATH variable"
#         echo " To fix this error:"
#         echo " - Omit the '-u' option and to install system-wide"
#         echo " - Specify an installation directory with -d <path>"
#         echo ""
#         exit 1
#       fi
#  
#       ;;
#     d)
#       # Get absolute path in case a relative path is provided
#       INSTALL_DIR=$(cd "$OPTARG"; pwd)
#  
#       if [ ! -d "$INSTALL_DIR" ]; then
#         text_title_error "Error"
#         echo " The installation directory '$INSTALL_DIR' does not exist or is not a directory"
#         echo ""
#         exit 1
#       fi
#  
#       ;;
#     \?)
#       text_title_error "Error"
#       echo " Invalid option: -$OPTARG" >&2
#       echo ""
#       exit 1
#       ;;
#     :)
#       text_title_error "Error"
#       echo " Option -$OPTARG requires an argument." >&2
#       echo ""
#       exit 1
#       ;;
#   esac
# done
# fi
#  
# cd "$(mktemp -d)"
#  
# text_title "Downloading Binary" " $DOWNLOAD_URL"
# curl -LO --proto '=https' --tlsv1.2 -sSf "$DOWNLOAD_URL"
#  
# text_title "Installing Binary" " $INSTALL_DIR/$NAME"
# chmod +x "$BINARY"
# mv "$BINARY" "$INSTALL_DIR/$NAME"
#  
# text_title "Installation Complete" " Run $NAME --help for more information"
# echo ""
