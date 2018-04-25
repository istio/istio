#!/bin/bash
case "$OSTYPE" in
  darwin*)  sh  cleanup_kubectl_host.sh
	;;
  linux*)   sh  rm_dockerdaemon_setup_linux.sh
	    sh  cleanup_kubectl_host.sh
	;;
  *)        echo "unsupported: $OSTYPE" 
	;;
esac
