#!/bin/bash

if [[ "${2}" == "-V=full" ]]; then
  "$@"
  exit 0
fi
case "$(basename ${1})" in
  link)
    # Output a dummy file
    touch "${3}"
    ;;
  *)
    "$@"
esac
