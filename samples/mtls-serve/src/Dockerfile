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

# build a mtls-server binary using the golang container
FROM golang:1.24 AS builder
WORKDIR /go/src/istio.io/mtls-server/
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags '-extldflags -static -s -w' -o mtls-server main.go

FROM scratch

WORKDIR /bin/
COPY --from=builder /go/src/istio.io/mtls-server/mtls-server .
ENTRYPOINT [ "/bin/mtls-server" ]
