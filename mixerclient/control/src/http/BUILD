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
    name = "control_lib",
    srcs = [
        "attributes_builder.cc",
        "attributes_builder.h",
        "client_context.h",
        "controller_impl.cc",
        "controller_impl.h",
        "request_handler_impl.cc",
        "request_handler_impl.h",
        "service_context.h"
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//control/include/http:headers_only",
        "//control/src:common_lib",
        "//control/src/utils:utils_lib",
    ],
)

cc_test(
    name = "attributes_builder_test",
    size = "small",
    srcs = [
        "mock_check_data.h",
        "mock_report_data.h",
        "attributes_builder_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":control_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "request_handler_impl_test",
    size = "small",
    srcs = [
        "mock_check_data.h",
        "mock_report_data.h",
        "request_handler_impl_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":control_lib",
        "//control/src:mock_mixer_client",
        "//external:googletest_main",
    ],
)
