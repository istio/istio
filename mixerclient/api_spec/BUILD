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
    name = "api_spec_lib",
    srcs = [
        "src/http_api_spec_parser_impl.cc",
        "src/http_api_spec_parser_impl.h",
        "src/http_template.cc",
        "src/http_template.h",
        "src/path_matcher.h",
        "src/path_matcher_node.cc",
        "src/path_matcher_node.h",
    ],
    hdrs = [
        "include/http_api_spec_parser.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@mixerapi_git//:mixer_client_config_cc_proto",
    ],
)

cc_test(
    name = "path_matcher_test",
    size = "small",
    srcs = ["src/path_matcher_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":api_spec_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "http_template_test",
    size = "small",
    srcs = ["src/http_template_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":api_spec_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "http_api_spec_parser_test",
    size = "small",
    srcs = ["src/http_api_spec_parser_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":api_spec_lib",
        "//:mixer_client_lib",
        "//external:googletest_main",
    ],
)
