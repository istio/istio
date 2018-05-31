#!/bin/bash

# This script wraps aws s3 cp to implement API expected by DOWNLOAD_COMMAND
if [ "$#" -ne 1 ]; then
    echo "Usage: $0 http_download url" 1>&2
    exit 1
fi
url="$1"


DOWNLOAD_COMMAND=""
set_download_command () {
    # Try curl.
    if command -v curl > /dev/null; then
        if curl --version | grep Protocols  | grep https > /dev/null; then
	       DOWNLOAD_COMMAND='curl -fLSs'
	       return
        fi
        echo curl does not support https, will try wget for downloading files.
    else
        echo curl is not installed, will try wget for downloading files.
    fi

    # Try wget.
    if command -v wget > /dev/null; then
        DOWNLOAD_COMMAND='wget -qO -'
        return
    fi
    echo wget is not installed.

    echo Error: curl is not installed or does not support https, wget is not installed. \
         Cannot download envoy. Please install wget or add support of https to curl.
    exit 1
}

set -e
set -x 


if [[ ${url} == s3:* ]]; then
  aws s3 cp "${url}" -
elif [[ ${url} =~ ^https?://.* ]]; then
  set_download_command 1>&2
  $DOWNLOAD_COMMAND ${url}
fi



