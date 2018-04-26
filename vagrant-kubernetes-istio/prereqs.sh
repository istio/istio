#!/bin/bash
case "$OSTYPE" in
  darwin*)  sh  macos_prereqs.sh;; 
  linux*)   sh  linux_prereqs.sh;; 
  *)        echo "unsupported: $OSTYPE" ;;
esac
