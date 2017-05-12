#!/bin/bash
# Temporary place holder that gets the job done.
# We can do arbitrary things in this script over time.
# This file will be fetched as: curl git.io/getIstio.sh | sh -
# The script fetches the latest Istio release and untars it.

# TODO: Automate me.
ISTIO_VERSION=0.1.1
URL=https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istio.tar.gz
echo "Downloading istio-$ISTIO_VERSION from $URL ..."
curl -L $URL | tar xz
# TODO: change this so the version is in the tgz/directory name (users trying multiple versions)
echo "Downloaded into istio:"
ls istio
