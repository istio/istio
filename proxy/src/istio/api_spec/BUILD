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
        "http_api_spec_parser_impl.cc",
        "http_api_spec_parser_impl.h",
        "http_template.cc",
        "http_template.h",
        "path_matcher.h",
        "path_matcher_node.cc",
        "path_matcher_node.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//external:mixer_client_config_cc_proto",
        "//include/istio/api_spec:headers_lib",
        "//include/istio/control/http:headers_lib",
    ],
)

cc_test(
    name = "path_matcher_test",
    size = "small",
    srcs = ["path_matcher_test.cc"],
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
    srcs = ["http_template_test.cc"],
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
    srcs = ["http_api_spec_parser_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":api_spec_lib",
        "//external:googletest_main",
        "//src/istio/control/http:mock_headers",
        "//src/istio/mixerclient:mixerclient_lib",
    ],
)
