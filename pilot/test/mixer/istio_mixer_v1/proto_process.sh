#!/bin/bash
#
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
#
# Rewrites mixer proto service definition by stripping gogoproto annotations
# and combining them into a single file for simplicity.
#
# gogoproto definitions depend on proto descriptor proto, which is hard to
# source for golang. It is an optimization that is not necessary for the
# service.
#

cat <<EOF
syntax = "proto3";
package istio.mixer.v1;
import "google/protobuf/duration.proto";
import "google/protobuf/timestamp.proto";
import "google/rpc/status.proto";
EOF

cat - |\
  sed '/^\/\//d' |\
  sed '/^package /d' |\
  sed '/^option (gogoproto/d' |\
  sed '/^import "/d' |\
  sed '/^syntax =/d' |\
  sed 's/\[.*\];$/;/'
