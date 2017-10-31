#!/bin/bash

# Install linters
go get -u github.com/alecthomas/gometalinter
gometalinter --install --vendored-linters
