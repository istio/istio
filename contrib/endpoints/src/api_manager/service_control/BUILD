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
package(default_visibility = ["//contrib/endpoints/src/api_manager:__subpackages__"])

cc_library(
    name = "service_control",
    srcs = [
        "aggregated.cc",
        "logs_metrics_loader.cc",
        "logs_metrics_loader.h",
        "proto.cc",
        "url.cc",
        "url.h",
    ],
    hdrs = [
        "aggregated.h",
        "info.h",
        "interface.h",
        "proto.h",
    ],
    linkopts = select({
        "//:darwin": [],
        "//conditions:default": [
            "-lm",
            "-luuid",
        ],
    }),
    deps = [
        "//contrib/endpoints/include:headers_only",
        "//contrib/endpoints/src/api_manager:impl_headers",
        "//contrib/endpoints/src/api_manager:server_config_proto",
        "//contrib/endpoints/src/api_manager/cloud_trace",
        "//contrib/endpoints/src/api_manager/utils",
        "//external:cc_wkt_protos",
        "//external:googletest_prod",
        "//external:grpc++",
        "//external:protobuf",
        "//external:service_config",
        "//external:servicecontrol",
        "//external:servicecontrol_client",
    ],
)

cc_test(
    name = "logs_metrics_loader_test",
    size = "small",
    srcs = [
        "logs_metrics_loader_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":service_control",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "proto_test",
    size = "small",
    srcs = [
        "proto_test.cc",
    ],
    data = glob(["testdata/*.golden"]),
    linkstatic = 1,
    deps = [
        ":service_control",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "url_test",
    size = "small",
    srcs = [
        "url_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":service_control",
        "//contrib/endpoints/src/api_manager:mock_api_manager_environment",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "aggregated_test",
    size = "small",
    srcs = [
        "aggregated_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":service_control",
        "//contrib/endpoints/src/api_manager:mock_api_manager_environment",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "check_response_test",
    size = "small",
    srcs = [
        "check_response_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":service_control",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "allocate_quota_response_test",
    size = "small",
    srcs = [
        "allocate_quota_response_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":service_control",
        "//external:googletest_main",
    ],
)
