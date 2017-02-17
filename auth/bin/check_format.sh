#!/bin/bash

unformatted_files=$(gofmt -l .)
if [ -n "$unformatted_files" ]; then
  echo "The following files are incorrectly formatted"
  for file in $unformatted_files; do
    echo "  " $file
  done
  exit 1
fi
