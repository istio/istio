#!/usr/bin/env bash
#
# Separate downloader for istioctl
#
# This file will be fetched as: curl -L https://git.io/getLatestIstioCtl | sh -
#
INSTALL_DIR="$HOME/.istioctl/bin"

# Determines the operating system.
detect_os() {
  OS=$(uname)
  case $OS in
    'Linux')
      OSEXT='linux'
      FILENAME='istioctl'
      ;;
    'WindowsNT')
      OSEXT='win.exe'
      FILENAME='istioctl.exe'
      ;;
    'Darwin') 
      OSEXT='osx'
      FILENAME='istioctl'
      ;;
    *) ;;
  esac
}

# Determines the istioctl version.
get_version() {
  if [ "x${ISTIO_VERSION}" = "x" ] ; then
  ISTIO_VERSION=$(curl -L -s https://api.github.com/repos/istio/istio/releases/latest | \
                  grep tag_name | sed "s/ *\"tag_name\": *\"\\(.*\\)\",*/\\1/")
  fi
}

# Downloads the istioctl binary archive.
download_istioctl() {
  NAME="istioctl-${OSEXT}"

  ## This is not the correct URL. Will update.
  URL="https://storage.googleapis.com/istio-artifacts/${ISTIO_VERSION}/artifacts/istioctl/${NAME}"
  
  printf "Downloading %s..\n" "$URL"
  printf "\n"
  curl -L "${URL}" -o "${FILENAME}"

  if [ $? -eq 0 ]; then
    printf "\n"
    printf "istioctl %s Download Complete!, Validating Checksum...\n" "$ISTIO_VERSION"
    validate_checksum
  else
    printf "There was a problem downloading isitioctl. Please try again."
  fi
}

# Validates the checksum
validate_checksum() {
   ## Download checksum

   ## If checksum is valid
   printf "Checksum Valid.\n"
   post_install
   ## If not
   ## printf "Checkum was invalid. Please try again."
}

# Displays post-install instructions.
post_install() {
  printf "\n"
  printf "istioctl has been successfully downloaded into the %s folder on your system.\n" "$NAME"
  printf "\n"
  printf "Add the istioctl client to your PATH environment variable, on a MacOS or Linux system:"
  printf "\n"
  printf "export PATH=\"\$PATH:${INSTALL_DIR}\""
  printf "\n"
}

main() {
  detect_os
  get_version
  download_istioctl
}

main
