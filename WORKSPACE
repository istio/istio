# Copyright 2016 Google Inc. All Rights Reserved.
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

# required by servicecontrol_client
git_repository(
    name = "boringssl",
    commit = "12c35d69008ae6b8486e435447445240509f7662",  # 2016-10-24
    remote = "https://boringssl.googlesource.com/boringssl",
)

bind(
    name = "boringssl_crypto",
    actual = "@boringssl//:crypto",
)

# Required by gRPC.
bind(
    name = "libssl",
    actual = "@boringssl//:ssl",
)

new_git_repository(
    name = "zlib_git",
    build_file = "third_party/BUILD.zlib",
    commit = "50893291621658f355bc5b4d450a8d06a563053d",  # v1.2.8
    remote = "https://github.com/madler/zlib.git",
)

bind(
    name = "zlib",
    actual = "@zlib_git//:zlib"
)

new_git_repository(
    name = "nanopb_git",
    build_file = "third_party/BUILD.nanopb",
    commit = "f8ac463766281625ad710900479130c7fcb4d63b",
    remote = "https://github.com/nanopb/nanopb.git",
)

bind(
    name = "nanopb",
    actual = "@nanopb_git//:nanopb",
)

git_repository(
    name = "grpc_git",
    commit = "d28417c856366df704200f544e72d31056931bce",
    remote = "https://github.com/grpc/grpc.git",
    init_submodules = True,
)

bind(
    name = "gpr",
    actual = "@grpc_git//:gpr",
)

bind(
    name = "grpc",
    actual = "@grpc_git//:grpc",
)

bind(
    name = "grpc_cpp_plugin",
    actual = "@grpc_git//:grpc_cpp_plugin",
)

bind(
    name = "grpc++",
    actual = "@grpc_git//:grpc++",
)

bind(
    name = "grpc_lib",
    actual = "@grpc_git//:grpc++_reflection",
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

bind(
    name = "protobuf_compiler",
    actual = "@protobuf_git//:protoc_lib",
)

bind(
    name = "protobuf_clib",
    actual = "@protobuf_git//:protobuf_lite",
)

new_git_repository(
    name = "googletest_git",
    build_file = "third_party/BUILD.googletest",
    commit = "d225acc90bc3a8c420a9bcd1f033033c1ccd7fe0",
    remote = "https://github.com/google/googletest.git",
)

bind(
    name = "googletest",
    actual = "@googletest_git//:googletest",
)

bind(
    name = "googletest_main",
    actual = "@googletest_git//:googletest_main",
)

bind(
    name = "googletest_prod",
    actual = "@googletest_git//:googletest_prod",
)

new_git_repository(
    name = "googleapis_git",
    commit = "db1d4547dc56a798915e0eb2c795585385922165",
    remote = "https://github.com/googleapis/googleapis.git",
    build_file = "third_party/BUILD.googleapis",
)

bind(
    name = "servicecontrol",
    actual = "@googleapis_git//:servicecontrol",
)

bind(
    name = "servicecontrol_genproto",
    actual = "@googleapis_git//:servicecontrol_genproto",
)

bind(
    name = "service_config",
    actual = "@googleapis_git//:service_config",
)

bind(
    name = "cloud_trace",
    actual = "@googleapis_git//:cloud_trace",
)

git_repository(
    name = "servicecontrol_client_git",
    commit = "d739d755365c6a13d0b4164506fd593f53932f5d",
    remote = "https://github.com/cloudendpoints/service-control-client-cxx.git",
)

bind(
    name = "servicecontrol_client",
    actual = "@servicecontrol_client_git//:service_control_client_lib",
)
