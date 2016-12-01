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
    name = "auth",
    srcs = [
        "jwt_cache.cc",
    ],
    hdrs = [
        "certs.h",
        "jwt_cache.h",
    ],
    linkopts = select({
        "//:darwin": [],
        "//conditions:default": [
            "-lm",
            "-luuid",
        ],
    }),
    deps = [
        "//external:googletest_prod",
        "//src/api_manager:auth_headers",
        "//src/api_manager/auth/lib",
        "//src/api_manager/utils",
        "//third_party/service-control-client-cxx:service_control_client_lib",
    ],
)

cc_library(
    name = "service_account_token",
    srcs = [
        "service_account_token.cc",
    ],
    hdrs = [
        "service_account_token.h",
    ],
    linkopts = select({
        "//:darwin": [],
        "//conditions:default": [
            "-lm",
            "-luuid",
        ],
    }),
    deps = [
        "//external:grpc++",
        "//include:headers_only",
        "//src/api_manager/auth/lib",
        "//src/api_manager/utils",
    ],
)

cc_test(
    name = "jwt_cache_test",
    size = "small",
    srcs = [
        "jwt_cache_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":auth",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "service_account_token_test",
    size = "small",
    srcs = [
        "service_account_token_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":service_account_token",
        "//external:googletest_main",
        "//src/api_manager:mock_api_manager_environment",
    ],
)
