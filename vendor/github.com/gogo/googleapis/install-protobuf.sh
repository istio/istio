#!/usr/bin/env bash

set -ex

cd /home/travis

basename=protoc-3.6.1-linux-x86_64
wget https://github.com/google/protobuf/releases/download/v3.6.1/$basename.zip
unzip $basename.zip

