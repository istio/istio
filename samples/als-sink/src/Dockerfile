# Copyright 2022 Istio Authors
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

# build a als-sink binary using the golang container
FROM golang:1.17
WORKDIR /go/src/istio.io/als-sink-server/
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build \
    -a --ldflags '-extldflags "-static"' -tags netgo -installsuffix netgo \
    -o build/sink main.go admin.go service.go
RUN apt-get update && apt-get --no-install-recommends install -y \
    curl && rm -rf /var/lib/apt/lists/*
RUN which curl

ENTRYPOINT [ "/go/src/istio.io/als-sink-server/build/sink" ]
EXPOSE 9001
EXPOSE 16000
