# Copyright 2016 Istio Authors. All Rights Reserved.
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

load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

cc_proto_library(
    name = "string_map_proto",
    srcs = ["string_map.proto"],
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    visibility = ["//visibility:public"],
)

cc_library(
    name = "filter_lib",
    srcs = [
        "config.cc",
        "config.h",
        "grpc_transport.cc",
        "grpc_transport.h",
        "http_filter.cc",
        "mixer_control.cc",
        "mixer_control.h",
        "tcp_filter.cc",
        "thread_dispatcher.cc",
        "thread_dispatcher.h",
        "utils.cc",
        "utils.h",
    ],
    deps = [
        ":string_map_proto",
        "//external:mixer_client_lib",
        "@envoy//source/exe:envoy_common_lib",
    ],
    alwayslink = 1,
)

cc_binary(
    name = "envoy",
    linkopts = [
        "-lrt",
        "-luuid",
    ],
    linkstatic = 1,
    visibility = ["//visibility:public"],
    deps = [
        ":filter_lib",
        "@envoy//source/exe:envoy_main_entry_lib",
    ],
)

pkg_tar(
    name = "envoy_tar",
    extension = "tar.gz",
    files = [":envoy"],
    mode = "0755",
    package_dir = "/usr/local/bin/",
)

pkg_tar(
    name = "start_envoy_tar",
    extension = "tar.gz",
    files = ["start_envoy"],
    mode = "0755",
    package_dir = "/usr/local/bin/",
)

pkg_tar(
    name = "config_tar",
    extension = "tar.gz",
    files = ["envoy.conf.template"],
    mode = "0666",
    package_dir = "/etc/opt/proxy",
    tags = ["manual"],
)
