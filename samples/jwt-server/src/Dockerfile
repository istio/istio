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

# build a jwt-server binary using the golang container
FROM golang:1.19 as builder
WORKDIR /go/src/istio.io/jwt-server/
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o jwt-server main.go

FROM gcr.io/distroless/static-debian11@sha256:21d3f84a4f37c36199fd07ad5544dcafecc17776e3f3628baf9a57c8c0181b3f as distroless

WORKDIR /bin/
# copy the jwt-server binary to a separate container based on BASE_DISTRIBUTION
COPY --from=builder /go/src/istio.io/jwt-server .
ENTRYPOINT [ "/bin/jwt-server" ]
EXPOSE 8000
