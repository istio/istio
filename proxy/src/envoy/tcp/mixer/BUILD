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

load(
    "@envoy//bazel:envoy_build_system.bzl",
    "envoy_cc_library",
)

envoy_cc_library(
    name = "filter_lib",
    srcs = [
        "config.h",
        "control.cc",
        "control.h",
        "control_factory.h",
        "filter.cc",
        "filter.h",
        "filter_factory.cc",
    ],
    repository = "@envoy",
    visibility = ["//visibility:public"],
    deps = [
        "//proxy/src/envoy/utils:utils_lib",
        "//proxy/src/istio/control/tcp:control_lib",
        "@envoy//source/exe:envoy_common_lib",
    ],
)
