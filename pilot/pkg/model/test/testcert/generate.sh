#!/bin/sh

# Copyright Istio Authors
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

openssl req -new -newkey rsa:4096 -x509 -sha256 \
        -days 3650 -nodes -out cert.pem -keyout key.pem \
        -subj "/C=US/ST=Denial/L=Ether/O=Dis/CN=localhost/SAN=localhost" \
        -addext "subjectAltName = DNS:localhost"

openssl req -new -newkey rsa:4096 -x509 -sha256 \
        -days 3650 -nodes -out cert2.pem -keyout key2.pem \
        -subj "/C=US/ST=Denial/L=Ether/O=Dis/CN=anotherhost" \
        -addext "subjectAltName = DNS:localhost"
