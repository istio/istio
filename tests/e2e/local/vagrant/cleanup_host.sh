#!/bin/bash

case "$OSTYPE" in
  darwin*)  sh  revert_kubectl_config_host.sh
	;;
  linux*)   sh  revert_dockerdaemon_linux.sh
	     sh  revert_kubectl_config_host.sh
	;;
  *)        echo "unsupported: $OSTYPE" 
	;;
esac
