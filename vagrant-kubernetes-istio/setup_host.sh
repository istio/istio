#!/bin/bash

case "$OSTYPE" in
  darwin*)  sh  setup_kubectl_config_host.sh
	;;
  linux*)   sh  setup_dockerdaemon_linux.sh
	    sh  setup_kubectl_config_host.sh
	;;
  *)        echo "unsupported: $OSTYPE" 
	;;
esac
