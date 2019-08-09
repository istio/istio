#!/bin/bash

if [[ -d "mtls-go-example" ]]; then
    rm -rf mtls-go-example
fi

if [[ -d "httpbin.example.com" ]]; then
    rm -rf httpbin.example.com
fi

if [[ -d "httpbin.new.example.com" ]]; then
    rm -rf httpbin.new.example.com
fi

if [[ -d "helloworld-v1.example.com" ]]; then
    rm -rf helloworld-v1.example.com
fi
