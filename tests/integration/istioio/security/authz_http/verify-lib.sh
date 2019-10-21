#!/usr/bin/env bash

# Copyright 2019 Istio Authors
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

set -e
set -u
set -o pipefail

REPEAT=${REPEAT:-100}
THRESHOLD=${THRESHOLD:-20}

ingress_url="http://istio-ingressgateway.istio-system/productpage"
sleep_pod=$(kubectl get pod -l app=sleep -n default -o 'jsonpath={.items..metadata.name}')

# verify calls curl to send requests to productpage via ingressgateway.
# - The 1st argument is the expected http response code
# - The remaining arguments are the expected text in the http response
# Return 0 if both the code and text is found in the response for continuously $THERESHOLD times,
# otherwise return 1.
#
# Examples:
# 1) Expect http code 200 and "reviews", "ratings" in the body: verify 200 "reviews" "ratings"
# 2) Expect http code 403 and "RBAC: access denied" in the body: verify 200 "RBAC: access denied"
# 3) Expect http code 200 only: verify 200
function verify {
  lastResponse=""
  wantCode=$1
  shift
  wantText=("$@")
  goodResponse=0

  for ((i=1; i<="$REPEAT"; i++)); do
    set +e
    response=$(kubectl exec "${sleep_pod}" -c sleep -n "default" -- curl "${ingress_url}" -s -w "\n%{http_code}\n")
    set -e
    mapfile -t respArray <<< "$response"
    code=${respArray[-1]}
    body=${response}

    matchedText=0
    if [ "$code" == "$wantCode" ]; then
      for want in "${wantText[@]}"; do
        if [[ "$body" = *$want* ]]; then
          matchedText=$((matchedText + 1))
        else
          lastResponse="$code\n$body"
        fi
      done
    else
      lastResponse="$code\n$body"
    fi

    if [[ "$matchedText" == "$#" ]]; then
      goodResponse=$((goodResponse + 1))
    else
      goodResponse=0
    fi

    if (( "$goodResponse">="$THRESHOLD" )); then
      return 0
    fi
  done


  echo -e "want code ${wantCode} and text: $(printf "%s, " "${wantText[@]}")\ngot: ${lastResponse}\n"
  return 1
}
