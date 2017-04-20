# Copyright 2017 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

licenses(["notice"])

load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

cc_library(
    name = "mixer_client_lib",
    srcs = [
        "src/attribute.cc",
        "src/attribute_context.cc",
        "src/attribute_context.h",
        "src/attribute_converter.h",
        "src/cache_key_set.cc",
        "src/cache_key_set.h",
        "src/check_cache.cc",
        "src/check_cache.h",
        "src/client_impl.cc",
        "src/client_impl.h",
        "src/context_update.cc",
        "src/context_update.h",
        "src/grpc_transport.cc",
        "src/grpc_transport.h",
        "src/signature.cc",
        "src/signature.h",
        "src/stream_transport.h",
        "src/transport.h",
        "utils/md5.cc",
        "utils/md5.h",
        "utils/status_test_util.h",
    ],
    hdrs = [
        "include/attribute.h",
        "include/client.h",
        "include/options.h",
        "include/transport.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        ":simple_lru_cache",
        "//external:boringssl_crypto",
        "//external:grpc++",
        "//external:mixer_api_cc_proto",
    ],
)

cc_library(
    name = "simple_lru_cache",
    srcs = ["utils/google_macros.h"],
    hdrs = [
        "utils/simple_lru_cache.h",
        "utils/simple_lru_cache_inl.h",
    ],
    visibility = ["//visibility:public"],
)

cc_test(
    name = "simple_lru_cache_test",
    size = "small",
    srcs = ["utils/simple_lru_cache_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":simple_lru_cache",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "attribute_test",
    size = "small",
    srcs = ["src/attribute_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "attribute_context_test",
    size = "small",
    srcs = ["src/attribute_context_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "context_update_test",
    size = "small",
    srcs = ["src/context_update_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "md5_test",
    size = "small",
    srcs = ["utils/md5_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "cache_key_set_test",
    size = "small",
    srcs = ["src/cache_key_set_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "signature_test",
    size = "small",
    srcs = ["src/signature_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "check_cache_test",
    size = "small",
    srcs = ["src/check_cache_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "grpc_transport_test",
    size = "small",
    srcs = ["src/grpc_transport_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
        "-lrt",
    ],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "stream_transport_test",
    size = "small",
    srcs = ["src/stream_transport_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
        "-lrt",
    ],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "client_impl_test",
    size = "small",
    srcs = ["src/client_impl_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
        "-lrt",
    ],
    linkstatic = 1,
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)
