# Copyright 2018 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# BASE_DISTRIBUTION is used to switch between the old base distribution and distroless base images
ARG BASE_DISTRIBUTION=default

# build a tcp-echo binary using the golang container
FROM golang:1.14.2 as builder
WORKDIR /go/src/istio.io/tcp-echo-server/
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tcp-echo main.go

# The following section is used as base image if BASE_DISTRIBUTION=default
# No tag available https://hub.docker.com/_/scratch?tab=description
# hadolint ignore=DL3006
FROM scratch as default

# The following section is used as base image if BASE_DISTRIBUTION=distroless
FROM gcr.io/distroless/static-debian10@sha256:4433370ec2b3b97b338674b4de5ffaef8ce5a38d1c9c0cb82403304b8718cde9 as distroless

# This will build the final image based on either default or distroless from above
# hadolint ignore=DL3006
FROM ${BASE_DISTRIBUTION:-debug}
WORKDIR /bin/
# copy the tcp-echo binary to a separate container based on BASE_DISTRIBUTION
COPY --from=builder /go/src/istio.io/tcp-echo-server/tcp-echo .
ENTRYPOINT [ "/bin/tcp-echo" ]
CMD [ "9000", "hello" ]
EXPOSE 9000
