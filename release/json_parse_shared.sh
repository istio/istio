#!/bin/bash
# Copyright 2017 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
################################################################################

set -o errexit
set -o nounset
set -o pipefail
set -x

# parse for a key and returns what follows after the : (sans leading whitespace)
function parse_json_for_line() {
  local FILENAME=$1
  local KEY=$2
  local RESULT=""

  local LINE_COUNT=$(grep -c -Eo " *\"${KEY}\": *.*,?" ${FILENAME})
  if [ "$LINE_COUNT" != "1" ]; then
    echo "Missing or ambiguous lines for ${KEY}: $LINE_COUNT" >&2
    return 0
  fi
  RESULT=$(grep -Eo " *\"${KEY}\": *.*,?" ${FILENAME} |
           sed "s/ *\"${KEY}\": *\(.*\)/\1/")
  echo $RESULT
  return 0
}

# parse for a key with an unquoted unsigned number and return the number
function parse_json_for_int() {
  local FILENAME=$1
  local KEY=$2
  local RESULT=""

  local LINE_COUNT=$(grep -c -Eo " *\"${KEY}\": *[0-9]*,?" ${FILENAME})
  if [ "$LINE_COUNT" != "1" ]; then
    echo "Missing or ambiguous lines for ${KEY}: $LINE_COUNT" >&2
    return 0
  fi
  RESULT=$(grep -Eo " *\"${KEY}\": *[0-9]*,?" ${FILENAME} |
           sed "s/ *\"${KEY}\": *\([0-9]*\),*/\1/")
  echo $RESULT
  return 0
}

# parse for a key with a quoted string and return the string contents
function parse_json_for_string() {
  local FILENAME=$1
  local KEY=$2
  local RESULT=""

  local LINE_COUNT=$(grep -c -Eo " *\"${KEY}\":.*?[^\\\\]\",?" ${FILENAME})
  if [ "$LINE_COUNT" != "1" ]; then
    echo "Missing or ambiguous lines for ${KEY}: $LINE_COUNT" >&2
    return 0
  fi
  RESULT=$(grep -Eo " *\"${KEY}\": *\".*\",?" ${FILENAME} |
           sed "s/ *\"${KEY}\": *\"\(.*\)\",*/\1/")
  echo $RESULT
  return 0
}

# parse for a key with a quoted string and return the string contents
# parse this when you expect multiple instances but will settle for the first
function parse_json_for_first_string() {
  local FILENAME=$1
  local KEY=$2
  local RESULT=""

  local LINE_COUNT=$(grep -c -Eo " *\"${KEY}\":.*?[^\\\\]\",?" ${FILENAME})
  if [ "$LINE_COUNT" == "0" ]; then
    echo "Missing line for ${KEY}: $LINE_COUNT" >&2
    return 0
  fi
  RESULT=$(grep -Eo " *\"${KEY}\": *\".*\",?" ${FILENAME} | head --lines=1 |
           sed "s/ *\"${KEY}\": *\"\(.*\)\",*/\1/")
  echo $RESULT
  return 0
}

# parses file #1 for URL in key #2 that ends in ...#3/(#4)*
function parse_json_for_url_suffix() {
  local FILENAME=$1
  local URL_KEY=$2
  local URL_SUFFIX=$3
  local VALID_CHARS=$4
  local RESULT=""

  local LINE_COUNT=$(grep -c -Eo " *\"${URL_KEY}\":.*?[^\\\\]\",?" ${FILENAME})
  if [ "$LINE_COUNT" == "0" ]; then
      return 0
  fi

  RESULT=$(grep -Eo " *\"${URL_KEY}\":.*${URL_SUFFIX}/(${VALID_CHARS})*\",?" ${FILENAME} | \
           sed "s# *\"${URL_KEY}\": *\".*${URL_SUFFIX}\/\(.*\)\",*#\1#")
  echo $RESULT
  return 0
}


# parses file #1 for URL in key #2 that ends in #3/integer, returns the integer
# #3 should just be a word without /
function parse_json_for_url_int_suffix() {
  local FILENAME=$1
  local URL_KEY=$2
  local URL_SUFFIX=$3
  local RESULT=""

  parse_json_for_url_suffix $* "[0-9]"
  return 0
}

# parses file #1 for URL in key #2 that ends in #3/hex, returns the hex
# #3 should just be a word without /
function parse_json_for_url_hex_suffix() {
  local FILENAME=$1
  local URL_KEY=$2
  local URL_SUFFIX=$3
  local RESULT=""

  parse_json_for_url_suffix $* "[0-9]|[a-f]|[A-F]"
  return 0
}
