#!/bin/bash

yq ea '. as $item ireduce ({}; . *d $item )' ./common/config/.golangci.yml ./tools/golangci-override.yaml > ./common/config/.golangci.yml.tmp
mv ./common/config/.golangci.yml.tmp ./common/config/.golangci.yml
