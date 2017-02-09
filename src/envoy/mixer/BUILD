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

load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@bazel_tools//tools/build_defs/docker:docker.bzl", "docker_build")

cc_library(
    name = "filter_lib",
    srcs = [
        "http_control.cc",
        "http_control.h",
        "http_filter.cc",
    ],
    deps = [
        "//external:mixer_client_lib",
        "@envoy_git//:envoy-common",
    ],
    alwayslink = 1,
)

cc_binary(
    name = "envoy",
    linkstatic = 1,
    deps = [
        ":filter_lib",
        "@envoy_git//:envoy-main",
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

docker_build(
    name = "proxy",
    base = "@docker_ubuntu//:xenial",
    entrypoint = [
        "/usr/local/bin/start_envoy",
        "-e",
        "/usr/local/bin/envoy",
        "-c",
        "/etc/opt/proxy/envoy.conf",
        "-t",
        "/etc/opt/proxy/envoy.conf.template",
    ],
    ports = ["9090"],
    repository = "istio",
    tags = ["manual"],
    tars = [
        ":config_tar",
        ":envoy_tar",
        ":start_envoy_tar",
    ],
)
