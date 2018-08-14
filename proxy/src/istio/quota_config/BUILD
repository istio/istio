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
    name = "config_parser_lib",
    srcs = [
        "config_parser_impl.cc",
        "config_parser_impl.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//external:mixer_client_config_cc_proto",
        "//proxy/include/istio/quota_config:headers_lib",
    ],
)

cc_test(
    name = "config_parser_impl_test",
    size = "small",
    srcs = ["config_parser_impl_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":config_parser_lib",
        "//external:googletest_main",
        "//proxy/include/istio/utils:headers_lib",
    ],
)
