#!/bin/bash
## Temporary place holder that gets the job done.
#We can do arbitrary things in this script over time.
## This file will be fetched as : curl git.io/getIstio.sh|sh -
## The script fetches the latest Istio release and untars it.

## Automate me.
ISTIO_VERSION=0.1.1

curl https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istio.tar.gz | tar xz
