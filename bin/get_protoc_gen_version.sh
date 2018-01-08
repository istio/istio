#!/usr/bin/env bash

ROOT=$GOPATH/src/istio.io/api

package_info=$(sed -n "/$1\/protobuf/,/\[\[projects/p" $ROOT/Gopkg.lock)

version=`echo $package_info | grep "version = " | sed -e 's/^.*version = \"//g' -e "s/\".*$//g"`
revision=`echo $package_info | grep "revision = " | sed -e 's/^.*revision = \"//g' -e "s/\".*$//g"`

if [[ ! -z "$version" ]]; then
  echo $version
else
  echo $revision
fi