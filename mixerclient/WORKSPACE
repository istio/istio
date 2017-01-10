# Copyright 2017 Google Inc. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# A Bazel (http://bazel.io) workspace for Istio Mixer client

git_repository(
 name = "boringssl",
 commit = "2f29d38cc5e6c1bfae4ce22b4b032fb899cdb705",  # 2016-07-12
 remote = "https://boringssl.googlesource.com/boringssl",
)

bind(
 name = "boringssl_crypto",
 actual = "@boringssl//:crypto",
)

git_repository(
 name = "protobuf_git",
 commit = "a428e42072765993ff674fda72863c9f1aa2d268",  # v3.1.0
 remote = "https://github.com/google/protobuf.git",
)

bind(
 name = "protoc",
 actual = "@protobuf_git//:protoc",
)

bind(
 name = "protobuf",
 actual = "@protobuf_git//:protobuf",
)

bind(
 name = "cc_wkt_protos",
 actual = "@protobuf_git//:cc_wkt_protos",
)

bind(
 name = "cc_wkt_protos_genproto",
 actual = "@protobuf_git//:cc_wkt_protos_genproto",
)

new_git_repository(
    name = "googleapis_git",
    commit = "6c1d6d4067364a21f8ffefa3401b213d652bf121", # common-protos-1_3_1
    remote = "https://github.com/googleapis/googleapis.git",
    build_file = "BUILD.googleapis",
)

bind(
    name = "service_config",
    actual = "@googleapis_git//:service_config",
)

bind(
    name = "service_config_genproto",
    actual = "@googleapis_git//:service_config_genproto",
)

new_git_repository(
    name = "mixerapi_git",
    commit = "fc5a396185edc72d06d1937f30a8148a37d4fc1b",
    remote = "https://github.com/istio/api.git",
    build_file = "BUILD.mixerapi",
)
bind(
    name = "mixer_api_cc_proto",
    actual = "@mixerapi_git//:mixer_api_cc_proto",
)