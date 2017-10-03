#!/bin/bash

res=$(grep -n commit WORKSPACE  | grep -v "#")

# found a commit line with no comment
if [[ ! -z $res ]]; then
  echo $res
  echo "Missing comment on dependency"
  echo "https://github.com/istio/istio/blob/master/devel/README.md#adding-dependencies"
  exit 1
fi

exit 0
