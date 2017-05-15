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
load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

exports_files(["integration.json"])

cc_proto_library(
    name = "bookstore_proto",
    srcs = ["bookstore.proto"],
    deps = [
        "@googleapis_git//:annotations",
    ],
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    visibility = ["//visibility:public"],
)

genrule(
    name = "bookstore_descriptor",
    srcs = [
        "@googleapis_git//:google/api/annotations.proto",
        "@googleapis_git//:google/api/http.proto",
        "@protobuf_git//:well_known_protos",
        "bookstore.proto",
    ],
    outs = ["bookstore.descriptor"],
    cmd = """$(location //external:protoc) $(location :bookstore.proto) \
        -I$$(dirname $(location @googleapis_git//:google/api/annotations.proto))/../.. \
        -Iexternal/protobuf_git/src \
        -I$$(dirname $(location :bookstore.proto)) \
        --include_imports --descriptor_set_out=$@""",
    tools = ["//external:protoc"],
    visibility = ["//visibility:public"],
)
