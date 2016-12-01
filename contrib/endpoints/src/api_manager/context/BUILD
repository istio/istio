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
    name = "context",
    srcs = [
        "request_context.cc",
        "service_context.cc",
    ],
    hdrs = [
        "request_context.h",
        "service_context.h",
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
        "//src/api_manager/auth",
        "//src/api_manager/auth:service_account_token",
        "//src/api_manager/cloud_trace",
        "//src/api_manager/service_control",
        "//src/api_manager/utils",
        "//external:cloud_trace",
        "//external:service_config",
        "//external:servicecontrol",
        "//third_party/service-control-client-cxx:service_control_client_lib",
    ],
)
