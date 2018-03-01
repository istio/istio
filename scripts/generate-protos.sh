#!/bin/bash

set -o errexit
set -o nounset

make clean clean-generated generate
