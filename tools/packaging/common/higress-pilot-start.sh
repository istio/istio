#!/bin/bash

if [ -n "$HIGRESS_CONTROLLER_SVC" ]; then
    # wait for mcp-bridge
    while true; do
        echo "testing higress controller is ready to connect..."
        nc -z $HIGRESS_CONTROLLER_SVC ${HIGRESS_CONTROLLER_PORT:-15051} 
        if [ $? -eq 0 ]; then
            break
        fi
        sleep 1
    done
fi

/usr/local/bin/pilot-discovery $*
