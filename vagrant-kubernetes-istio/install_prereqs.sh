#!/bin/bash

case "$OSTYPE" in
  darwin*)  sh  install_prereqs_macos.sh;; 
  linux*)   sh  install_prereqs_linux.sh;; 
  *)        echo "unsupported: $OSTYPE" ;;
esac
