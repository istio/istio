#!/usr/bin/env bash

if [[ -d "mtls-go-example" ]]; then
    rm -rf mtls-go-example
fi

git clone https://github.com/nicholasjackson/mtls-go-example

pushd mtls-go-example

sh ./generate.sh httpbin.example.com password

mkdir ../httpbin.example.com && mv 1_root 2_intermediate 3_application 4_client ../httpbin.example.com

popd