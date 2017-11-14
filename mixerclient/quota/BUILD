# Copyright 2017 Istio Authors. All Rights Reserved.
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

licenses(["notice"])

cc_library(
    name = "requirement_header",
    hdrs = [
        "include/requirement.h",
    ],
    visibility = ["//visibility:public"],
)

cc_library(
    name = "config_parser_lib",
    srcs = [
        "src/config_parser_impl.cc",
        "src/config_parser_impl.h",
    ],
    hdrs = [
        "include/config_parser.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        ":requirement_header",
        "@mixerapi_git//:mixer_client_config_cc_proto",
    ],
)

cc_test(
    name = "config_parser_impl_test",
    size = "small",
    srcs = ["src/config_parser_impl_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":config_parser_lib",
        "//:mixer_client_lib",
        "//external:googletest_main",
    ],
)
