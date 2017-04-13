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
cc_library(
    name = "envoy_input_stream",
    srcs = [
        "envoy_input_stream.cc",
    ],
    hdrs = [
        "envoy_input_stream.h",
    ],
    deps = [
        "//contrib/endpoints/src/grpc/transcoding:transcoder_input_stream",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

cc_test(
    name = "envoy_input_stream_test",
    srcs = [
        "envoy_input_stream_test.cc",
    ],
    deps = [
        ":envoy_input_stream",
        "@googletest_git//:googletest_main",
    ],
)

cc_library(
    name = "filter_lib",
    srcs = [
        "config.cc",
        "config.h",
        "filter.cc",
    ],
    deps = [
        ":envoy_input_stream",
        "//contrib/endpoints/src/grpc/transcoding",
        "@envoy//source/exe:envoy_common_lib",
    ],
    alwayslink = 1,
)

cc_binary(
    name = "envoy",
    linkstatic = 1,
    visibility = [":__subpackages__"],
    deps = [
        ":filter_lib",
        "@envoy//source/exe:envoy_main_lib",
    ],
    linkopts = [
        "-lrt",
    ],
)
