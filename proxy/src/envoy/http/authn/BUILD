# Copyright 2018 Istio Authors. All Rights Reserved.
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
    "envoy_cc_library",
    "envoy_cc_test",
    "envoy_cc_test_library",
)

envoy_cc_library(
    name = "authenticator",
    srcs = [
        "authenticator_base.cc",
        "authn_utils.cc",
        "filter_context.cc",
        "origin_authenticator.cc",
        "peer_authenticator.cc",
    ],
    hdrs = [
        "authenticator_base.h",
        "authn_utils.h",
        "filter_context.h",
        "origin_authenticator.h",
        "peer_authenticator.h",
    ],
    repository = "@envoy",
    deps = [
        "//external:authentication_policy_config_cc_proto",
        "//proxy/src/envoy/http/jwt_auth:jwt_lib",
        "//proxy/src/envoy/utils:utils_lib",
        "//proxy/src/istio/authn:context_proto",
        "//proxy/src/envoy/utils:filter_names_lib",
    ],
)

envoy_cc_library(
    name = "filter_lib",
    srcs = [
        "http_filter.cc",
        "http_filter_factory.cc",
    ],
    hdrs = [
        "http_filter.h",
    ],
    repository = "@envoy",
    deps = [
        ":authenticator",
        "//external:authentication_policy_config_cc_proto",
        "//proxy/src/envoy/utils:authn_lib",
        "//proxy/src/envoy/utils:utils_lib",
        "//proxy/src/istio/authn:context_proto",
        "@envoy//source/exe:envoy_common_lib",
        "//proxy/src/envoy/utils:filter_names_lib",
    ],
)

envoy_cc_test_library(
    name = "test_utils",
    hdrs = ["test_utils.h"],
    repository = "@envoy",
    deps = [
        "//proxy/src/istio/authn:context_proto",
    ],
)

envoy_cc_test(
    name = "filter_context_test",
    srcs = ["filter_context_test.cc"],
    repository = "@envoy",
    deps = [
        ":authenticator",
        ":test_utils",
        "@envoy//test/mocks/http:http_mocks",
        "@envoy//test/test_common:utility_lib",
    ],
)

envoy_cc_test(
    name = "authenticator_base_test",
    srcs = ["authenticator_base_test.cc"],
    repository = "@envoy",
    deps = [
        ":authenticator",
        ":test_utils",
        "@envoy//test/mocks/network:network_mocks",
        "//proxy/src/envoy/utils:filter_names_lib",
        "@envoy//test/mocks/ssl:ssl_mocks",
        "@envoy//test/test_common:utility_lib",
    ],
)

envoy_cc_test(
    name = "authn_utils_test",
    srcs = ["authn_utils_test.cc"],
    repository = "@envoy",
    deps = [
        ":authenticator",
        ":test_utils",
        "@envoy//test/test_common:utility_lib",
    ],
)

envoy_cc_test(
    name = "peer_authenticator_test",
    srcs = ["peer_authenticator_test.cc"],
    repository = "@envoy",
    deps = [
        ":authenticator",
        ":test_utils",
        "@envoy//test/mocks/http:http_mocks",
        "@envoy//test/test_common:utility_lib",
    ],
)

envoy_cc_test(
    name = "origin_authenticator_test",
    srcs = ["origin_authenticator_test.cc"],
    repository = "@envoy",
    deps = [
        ":authenticator",
        ":test_utils",
        "@envoy//test/mocks/http:http_mocks",
        "@envoy//test/test_common:utility_lib",
    ],
)

envoy_cc_test(
    name = "http_filter_test",
    srcs = ["http_filter_test.cc"],
    repository = "@envoy",
    deps = [
        ":filter_lib",
        ":test_utils",
        "//external:authentication_policy_config_cc_proto",
        "@envoy//source/common/http:header_map_lib",
        "@envoy//test/mocks/http:http_mocks",
        "@envoy//test/test_common:utility_lib",
    ],
)

envoy_cc_test(
    name = "http_filter_integration_test",
    srcs = ["http_filter_integration_test.cc"],
    data = glob(["testdata/*"]),
    repository = "@envoy",
    deps = [
        ":filter_lib",
        "@envoy//source/common/common:utility_lib",
        "@envoy//test/integration:http_protocol_integration_lib",
        "//proxy/src/envoy/utils:filter_names_lib",
    ],
)
