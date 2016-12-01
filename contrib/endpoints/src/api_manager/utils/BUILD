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
