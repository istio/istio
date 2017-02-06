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

package(default_visibility = ["//visibility:public"])

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
    name = "envoy_esp",
    linkstatic = 1,
    deps = [
        ":filter_lib",
        "@envoy_git//:envoy-main",
    ],
)
