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
package(default_visibility = [
    "//src/api_manager:__subpackages__",
    "//test/grpc:__subpackages__",
])

load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

cc_library(
    name = "utils",
    srcs = [
        "marshalling.cc",
        "status.cc",
        "url_util.cc",
    ],
    hdrs = [
        "marshalling.h",
        "stl_util.h",
        "url_util.h",
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
        "//external:protobuf",
        "//include:headers_only",
        "//external:servicecontrol",  # for google/rpc/status.proto
    ],
)

cc_test(
    name = "marshalling_test",
    size = "small",
    srcs = [
        "marshalling_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":utils",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "status_test",
    size = "small",
    srcs = [
        "status_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":utils",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "url_util_test",
    size = "small",
    srcs = [
        "url_util_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":utils",
        "//external:googletest_main",
    ],
)
