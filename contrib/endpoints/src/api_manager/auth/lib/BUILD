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
cc_library(
    name = "lib",
    srcs = [
        "auth_jwt_validator.cc",
        "auth_token.cc",
        "base64.cc",
        "grpc_internals.h",
        "json.cc",
        "json_util.cc",
    ],
    hdrs = [
        "auth_jwt_validator.h",
        "auth_token.h",
        "base64.h",
        "json.h",
        "json_util.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//external:grpc",
        "//external:protobuf",
        "//include:headers_only",
        "//src/api_manager:auth_headers",
        "//src/api_manager/utils",
    ],
)

cc_test(
    name = "auth_jwt_validator_test",
    size = "small",
    srcs = [
        "auth_jwt_validator_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "auth_token_test",
    size = "small",
    srcs = [
        "auth_token_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "json_test",
    size = "small",
    srcs = [
        "json_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "base64_test",
    size = "small",
    srcs = [
        "base64_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "json_util_test",
    size = "small",
    srcs = [
        "json_util_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":lib",
        "//external:googletest_main",
    ],
)
