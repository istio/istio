#!/bin/bash

folderName="httpbin.example.com"
if [[ $1 != "" ]]; then
        folderName=$1
fi
cn="httpbin.example.com"
if [[ $2 != "" ]]; then
        cn=$2
fi

echo "Generate client and server certificates and keys"

if [[ ! -d "mtls-go-example" ]]; then
    git clone https://github.com/nicholasjackson/mtls-go-example
    # Bypass prompt to enter yes when signing certificates.
    sed -i 's/openssl ca /openssl ca -batch /g' mtls-go-example/generate.sh
fi

pushd mtls-go-example || exit

./generate.sh "${cn}" password

mkdir ../"${folderName}" && mv 1_root 2_intermediate 3_application 4_client ../"${folderName}"

popd || exit