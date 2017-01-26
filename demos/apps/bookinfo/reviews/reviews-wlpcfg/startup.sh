#!/bin/bash
echo A8_ENDPOINT_TYPE=${A8_ENDPOINT_TYPE}
echo A8_ENDPOINT_PORT=${A8_ENDPOINT_PORT}
exec a8sidecar /opt/ibm/wlp/bin/server run defaultServer
