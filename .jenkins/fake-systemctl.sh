#!/bin/bash
if [[ "$@" == "is-active kubelet localkube" ]]; then
  exit 1
fi
exit 0
