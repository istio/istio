# Copyright (C) Extensible Service Proxy Authors
# All rights reserved.
#
# Redistribution and use in source and binary forms, with or without
# modification, are permitted provided that the following conditions
# are met:
# 1. Redistributions of source code must retain the above copyright
#    notice, this list of conditions and the following disclaimer.
# 2. Redistributions in binary form must reproduce the above copyright
#    notice, this list of conditions and the following disclaimer in the
#    documentation and/or other materials provided with the distribution.
#
# THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
# ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
# IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
# ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
# FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
# DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
# OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
# HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
# LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
# OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
# SUCH DAMAGE.
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
    visibility = [
        ":__subpackages__",
        # TODO: Consider using a top-level proto directory for protos that are
        # visible to NGINX module.
        "//src/nginx:__subpackages__",
    ],
)

cc_proto_library(
    name = "status_proto",
    srcs = [
        "proto/api_manager_status.proto",
    ],
    include = ".",
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    # TODO: Consider using a top-level proto directory for protos that are
    # visible to NGINX module.
    visibility = ["//src/nginx:__subpackages__"],
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
        "http_template.h",
        "method_impl.h",
        "path_matcher.h",
        "path_matcher_node.h",
        "request_handler.h",
    ],
    deps = [
        "//include:headers_only",
    ],
)

# TODO: Move check_auth and check_service_control into sub-directories once
# cyclic dependency is resolved
cc_library(
    name = "api_manager",
    srcs = [
        "api_manager_impl.cc",
        "check_auth.cc",
        "check_auth.h",
        "check_service_control.cc",
        "check_service_control.h",
        "check_workflow.cc",
        "check_workflow.h",
        "config.cc",
        "fetch_metadata.cc",
        "fetch_metadata.h",
        "gce_metadata.cc",
        "http_template.cc",
        "http_template.h",
        "method_impl.cc",
        "path_matcher.cc",
        "path_matcher_node.cc",
        "request_handler.cc",
    ],
    linkopts = select({
        "//:darwin": [],
        "//conditions:default": [
            "-lm",
            "-luuid",
        ],
    }),
    visibility = [
        "//include:__subpackages__",
        #TODO: subpackages shouldn't depends on this target
        ":__subpackages__",
    ],
    deps = [
        ":auth_headers",
        ":impl_headers",
        ":server_config_proto",
        "//external:cc_wkt_protos",
        "//external:googletest_prod",
        "//external:grpc++",
        "//external:protobuf",
        "//src/api_manager/auth",
        "//src/api_manager/cloud_trace",
        "//src/api_manager/context",
        "//src/api_manager/service_control",
        "//src/api_manager/utils",
        "//external:cloud_trace",
        "//external:service_config",
        "//external:servicecontrol",
        "//third_party/service-control-client-cxx:service_control_client_lib",
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
        ":api_manager",
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
        "path_matcher_test.cc",
        "mock_method_info.h"
    ],
    linkstatic = 1,
    deps = [
        ":api_manager",
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

cc_inc_library(
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
