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
package(default_visibility = ["//src/api_manager:__subpackages__"])

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
        "//external:cc_wkt_protos",
        "//external:googletest_prod",
        "//external:grpc++",
        "//external:protobuf",
        "//src/api_manager:impl_headers",
        "//src/api_manager:server_config_proto",
        "//src/api_manager/auth:service_account_token",
        "//src/api_manager/auth/lib",
        "//src/api_manager/cloud_trace",
        "//src/api_manager/utils",
        "//external:service_config",
        "//external:servicecontrol",
        "//third_party/service-control-client-cxx:service_control_client_lib",
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
        "//external:googletest_main",
        "//src/api_manager:mock_api_manager_environment",
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
        "//external:googletest_main",
        "//src/api_manager:mock_api_manager_environment",
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
