#!/usr/bin/env bash

supported_releases="1.8"

for release in $supported_releases
do
  releases=$(git tag -l "$release*" | grep -v rc | grep -v alpha)
  for rel in $releases
  do
    git checkout $rel
    cp -R manifests/charts /manifests/$rel
  done
done
