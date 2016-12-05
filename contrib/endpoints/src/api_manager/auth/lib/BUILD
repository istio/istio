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
        "//contrib/endpoints/include:headers_only",
        "//contrib/endpoints/src/api_manager:auth_headers",
        "//contrib/endpoints/src/api_manager/utils",
        "//external:grpc",
        "//external:protobuf",
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
