#!/bin/bash

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
# Return 0 (success) if both the code and text is found in the response for continuously $THERESHOLD times.
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
    response=$(kubectl exec ${sleep_pod} -c sleep -n "default" -- curl ${ingress_url} -s -w "\n%{http_code}\n")
    respArray=(${response[@]})
    code=${respArray[-1]}
    body=${respArray[@]::${#respArray[@]}-1}

    matchedText=0
    if [[ "$code" == "$wantCode" ]]; then
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
      exit 0
    fi
  done

  echo -e "want: ${wantCode}, ${wantText[@]}\ngot: ${lastResponse}\n"
  exit 1
}
