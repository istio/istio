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
package(default_visibility = [":__subpackages__"])

load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

cc_proto_library(
    name = "server_config_proto",
    srcs = [
        "proto/server_config.proto",
    ],
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    visibility = ["//visibility:public"],
)

cc_proto_library(
    name = "status_proto",
    srcs = [
        "proto/api_manager_status.proto",
    ],
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    visibility = ["//visibility:public"],
)

cc_proto_library(
    name = "security_rules_proto",
    srcs = [
        "proto/security_rules.proto",
    ],
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    visibility = ["//visibility:public"],
    deps = [
        "//external:cc_wkt_protos",
    ],
)

cc_library(
    name = "auth_headers",
    hdrs = [
        "auth.h",
    ],
)

cc_library(
    name = "impl_headers",
    hdrs = [
        "api_manager_impl.h",
        "config.h",
        "gce_metadata.h",
        "method_impl.h",
        "request_handler.h",
    ],
    deps = [
        "//contrib/endpoints/include:headers_only",
    ],
)

cc_library(
    name = "api_manager",
    srcs = [
        "api_manager_impl.cc",
        "check_auth.cc",
        "check_auth.h",
        "check_security_rules.cc",
        "check_security_rules.h",
        "check_service_control.cc",
        "check_service_control.h",
        "check_workflow.cc",
        "check_workflow.h",
        "config.cc",
        "config_manager.cc",
        "config_manager.h",
        "fetch_metadata.cc",
        "fetch_metadata.h",
        "gce_metadata.cc",
        "http_template.h",
        "method_impl.cc",
        "quota_control.cc",
        "quota_control.h",
        "request_handler.cc",
        "service_management_fetch.cc",
        "service_management_fetch.h",
        "weighted_selector.cc",
        "weighted_selector.h",
    ],
    linkopts = select({
        "//:darwin": [],
        "//conditions:default": [
            "-lm",
            "-luuid",
        ],
    }),
    visibility = [
        "//visibility:public",
    ],
    deps = [
        ":auth_headers",
        ":http_template",
        ":impl_headers",
        ":path_matcher",
        ":security_rules_proto",
        ":server_config_proto",
        "//contrib/endpoints/src/api_manager/auth",
        "//contrib/endpoints/src/api_manager/cloud_trace",
        "//contrib/endpoints/src/api_manager/context",
        "//contrib/endpoints/src/api_manager/firebase_rules",
        "//contrib/endpoints/src/api_manager/service_control",
        "//contrib/endpoints/src/api_manager/utils",
        "//external:cc_wkt_protos",
        "//external:cloud_trace",
        "//external:googletest_prod",
        "//external:grpc++",
        "//external:protobuf",
        "//external:service_config",
        "//external:servicecontrol",
        "//external:servicecontrol_client",
    ],
)

cc_library(
    name = "path_matcher",
    srcs = [
        "path_matcher_node.cc",
        "path_matcher_node.h",
    ],
    hdrs = [
        "path_matcher.h",
    ],
    visibility = [
        "//visibility:public",
    ],
    deps = [
        ":http_template",
    ],
)

cc_library(
    name = "http_template",
    srcs = [
        "http_template.cc",
    ],
    hdrs = [
        "http_template.h",
    ],
    visibility = [
        "//visibility:public",
    ],
)

cc_test(
    name = "config_test",
    size = "small",
    srcs = [
        "config_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
        ":mock_api_manager_environment",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "gce_metadata_test",
    size = "small",
    srcs = [
        "gce_metadata_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "api_manager_test",
    size = "small",
    srcs = [
        "api_manager_test.cc",
    ],
    data = glob(["testdata/*.json"]),
    linkstatic = 1,
    deps = [
        ":api_manager",
        ":mock_api_manager_environment",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "service_management_fetch_test",
    size = "small",
    srcs = [
        "mock_request.h",
        "service_management_fetch_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
        ":mock_api_manager_environment",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "http_template_test",
    size = "small",
    srcs = [
        "http_template_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":http_template",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "method_test",
    size = "small",
    srcs = [
        "method_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "path_matcher_test",
    size = "small",
    srcs = [
        "mock_method_info.h",
        "path_matcher_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":path_matcher",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "common_protos_test",
    size = "small",
    srcs = [
        "common_protos_test.cc",
    ],
    deps = [
        "//external:googletest_main",
        "//external:service_config",
        "//external:servicecontrol",
    ],
)

cc_test(
    name = "weighted_selector_test",
    size = "small",
    srcs = [
        "weighted_selector_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "server_config_proto_test",
    size = "small",
    srcs = [
        "server_config_proto_test.cc",
    ],
    data = [
        "proto/sample_server_config.pb.txt",
    ],
    deps = [
        ":server_config_proto",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "config_manager_test",
    size = "small",
    srcs = [
        "config_manager.h",
        "config_manager_test.cc",
    ],
    deps = [
        ":api_manager",
        ":mock_api_manager_environment",
        "//external:googletest_main",
    ],
)

cc_library(
    name = "mock_api_manager_environment",
    testonly = True,
    hdrs = [
        "mock_api_manager_environment.h",
    ],
    deps = [
        # TODO: remove this dependency
        ":api_manager",
    ],
)

cc_test(
    name = "check_auth_test",
    size = "small",
    srcs = [
        "check_auth_test.cc",
        "mock_request.h",
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
        ":mock_api_manager_environment",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "fetch_metadata_test",
    size = "small",
    srcs = [
        "fetch_metadata_test.cc",
        "mock_request.h",
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
        ":mock_api_manager_environment",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "check_security_rules_test",
    size = "small",
    srcs = [
        "check_security_rules_test.cc",
        "mock_request.h",
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
        ":mock_api_manager_environment",
        ":security_rules_proto",
        "//external:cc_wkt_protos",
        "//external:googletest_main",
    ],
)
