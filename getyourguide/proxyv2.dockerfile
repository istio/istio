FROM golang:1.11.13-alpine AS builder

COPY . /go/src/istio.io/istio/

WORKDIR /go/src/istio.io/istio/

RUN apk add gcc make bash git curl

# use envoy proxy version from 1.1.17 as 1.1.8 doesn't exist anymore
RUN PROXY_REPO_SHA=4157a97bd82940cff8f775c42ac00c5219d5609b bash -c "make pilot-agent"

FROM istio/proxyv2:1.1.8

COPY --from=builder /go/out/linux_amd64/release/pilot-agent /usr/local/bin/pilot-agent
