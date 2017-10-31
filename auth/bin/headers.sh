#!/bin/bash

exit_status=0

for file in $(git ls-files | grep  -e '.*\.go'); do
  head -n 1 $file | grep -P '^// Copyright 20\d{2} Istio Authors' > /dev/null
  if [[ $? -ne 0 ]]; then
    echo $file does not have a copyright license header

    # Set exit_status to 1 to indicate the check fails.
    exit_status=1
  fi
done

exit $exit_status
