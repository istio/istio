#!/bin/bash

SCRIPTPATH=$(cd "$(dirname "$0")" && pwd)
ROOTDIR=$(cd "$(dirname "${SCRIPTPATH}")" && pwd)
pushd "$ROOTDIR"

ret=0
for fn in $(find "${ROOTDIR}" -name '*.go' | grep -v vendor | grep -v testdata); do
  if [[ "$fn" == *.pb.go ]];then
    continue
  fi

  if head -20 "$fn" | grep "auto\-generated" > /dev/null; then
    continue
  fi

  if head -20 "$fn" | grep "DO NOT EDIT" > /dev/null; then
    continue
  fi

  if ! head -20 "$fn" | grep "Apache License, Version 2" > /dev/null; then
    echo "${fn} missing license"
    ret=$((ret+1))
  fi

  if ! head -20 "$fn" | grep Copyright > /dev/null; then
    echo "${fn} missing Copyright"
    ret=$((ret+1))
  fi
done

popd
exit "$ret"
