#!/bin/bash

# Set up env ISTIO if not done yet 
if [[ -z "${ISTIO// }" ]]; then
    export ISTIO=$GOPATH/src/istio.io
    echo 'Set ISTIO to' "$ISTIO"
fi

case "$OSTYPE" in
  darwin*)  sh  setup_kubectl_config_host.sh
	;;
  linux*)   sh  setup_dockerdaemon_linux.sh
	     sh  setup_kubectl_config_host.sh
	;;
  *)        echo "unsupported: $OSTYPE" 
	;;
esac
