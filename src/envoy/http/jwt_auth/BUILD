# Copyright 2017 Istio Authors. All Rights Reserved.
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

package(default_visibility = ["//visibility:public"])

load(
    "@envoy//bazel:envoy_build_system.bzl",
    "envoy_cc_binary",
    "envoy_cc_library",
    "envoy_cc_test",
)

load(
    "@envoy_api//bazel:api_build_system.bzl",
    "api_proto_library",
)

api_proto_library(
    name = "config_proto",
    srcs = [":config.proto"],
    visibility = ["//visibility:public"],
    require_py = 0,
)

envoy_cc_library(
    name = "jwt_lib",
    srcs = ["jwt.cc"],
    hdrs = ["jwt.h"],
    repository = "@envoy",
    deps = [
        "//external:rapidjson",
        "//external:ssl",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

envoy_cc_library(
    name = "jwt_authenticator_lib",
    srcs = [
        "jwt_authenticator.cc",
        "token_extractor.cc",
    ],
    hdrs = [
        "auth_store.h",
        "jwt_authenticator.h",
        "pubkey_cache.h",
        "token_extractor.h",
    ],
    repository = "@envoy",
    deps = [
        ":config_proto_cc",
        ":jwt_lib",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

envoy_cc_library(
    name = "http_filter_lib",
    srcs = [
        "http_filter.cc",
    ],
    hdrs = [
        "http_filter.h",
    ],
    repository = "@envoy",
    deps = [
        ":jwt_authenticator_lib",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

envoy_cc_library(
    name = "http_filter_factory",
    srcs = ["http_filter_factory.cc"],
    repository = "@envoy",
    deps = [
        ":http_filter_lib",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

envoy_cc_test(
    name = "http_filter_integration_test",
    srcs = [":integration_test/http_filter_integration_test.cc"],
    data = [
        "integration_test/envoy.conf.jwk",
    ],
    repository = "@envoy",
    deps = [
        ":http_filter_factory",
        ":jwt_lib",
        "@envoy//test/integration:http_integration_lib",
        "@envoy//test/integration:integration_lib",
    ],
)

envoy_cc_test(
    name = "jwt_test",
    srcs = [
        "jwt_test.cc",
    ],
    data = [],
    repository = "@envoy",
    deps = [
        ":jwt_lib",
        "@envoy//source/exe:envoy_common_lib",
        "@envoy//test/test_common:utility_lib",
    ],
)

envoy_cc_test(
    name = "jwt_authenticator_test",
    srcs = [
        "jwt_authenticator_test.cc",
    ],
    data = [],
    repository = "@envoy",
    deps = [
        ":jwt_authenticator_lib",
        "@envoy//source/exe:envoy_common_lib",
        "@envoy//test/mocks/upstream:upstream_mocks",
        "@envoy//test/test_common:utility_lib",
    ],
)

envoy_cc_test(
    name = "token_extractor_test",
    srcs = [
        "token_extractor_test.cc",
    ],
    data = [],
    repository = "@envoy",
    deps = [
        ":jwt_authenticator_lib",
        "@envoy//source/exe:envoy_common_lib",
        "@envoy//test/test_common:utility_lib",
    ],
)
