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
load(
    "@envoy//bazel:envoy_build_system.bzl",
    "envoy_proto_library",
    "envoy_cc_library",
    "envoy_cc_binary",
    "envoy_cc_test",
    "envoy_linkopts",
)

envoy_cc_library(
    name = "quota_config_lib",
    srcs = [
        "quota_config.cc",
        "quota_config.h",
    ],
    repository = "@envoy",
    deps = [
        "//external:mixer_client_lib",
	"@mixerapi_git//:mixer_client_config_cc_proto",
    ],
)

envoy_cc_test(
    name = "quota_config_test",
    srcs = [
        "quota_config_test.cc",
    ],
    repository = "@envoy",
    deps = [
        ":quota_config_lib",
    ],
)

envoy_cc_library(
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
        "utils.cc",
        "utils.h",
    ],
    repository = "@envoy",
    deps = [
        "//external:mixer_client_lib",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

envoy_cc_binary(
    name = "envoy",
    repository = "@envoy",
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
