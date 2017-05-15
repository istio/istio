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
cc_library(
    name = "envoy_input_stream",
    srcs = [
        "envoy_input_stream.cc",
    ],
    hdrs = [
        "envoy_input_stream.h",
    ],
    deps = [
        "//external:transcoding",
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
        "filter.h",
    ],
    deps = [
        ":envoy_input_stream",
        "//external:path_matcher",
        "//external:transcoding",
        "@googleapis_git//:annotations",
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

cc_test(
    name = "filter_test",
    srcs = [
        "filter_test.cc",
    ],
    deps = [
        "filter_lib",
        "//src/envoy/transcoding/test:bookstore_proto",
        "@envoy//source/common/grpc:codec_lib",
        "@envoy//test/mocks/http:http_mocks",
        "@envoy//test/mocks/upstream:upstream_mocks",
        "@envoy//test/test_common:utility_lib",
        "@envoy//test:main",
    ],
    data = [
        "//src/envoy/transcoding/test:bookstore_descriptor",
    ],
)

cc_test(
    name = "integration_test",
    srcs = [
        "integration_test.cc",
    ],
    deps = [
        "filter_lib",
        "//src/envoy/transcoding/test:bookstore_proto",
        "@envoy//source/common/grpc:codec_lib",
        "@envoy//test/integration:integration_lib",
        "@envoy//test/mocks/http:http_mocks",
        "@envoy//test/mocks/upstream:upstream_mocks",
        "@envoy//test/test_common:utility_lib",
        "@envoy//test:main",
    ],
    data = [
        "//src/envoy/transcoding/test:bookstore_descriptor",
        "//src/envoy/transcoding/test:integration.json",
    ],
)
