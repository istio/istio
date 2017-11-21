#!/bin/bash

cd /tmp
wget https://redirector.gvt1.com/edgedl/go/go1.9.2.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.9.2.linux-amd64.tar.gz
sudo chown -R circleci /usr/local/go
