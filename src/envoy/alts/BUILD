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
load(
    "@envoy//bazel:envoy_build_system.bzl",
    "envoy_cc_library",
)

load(
    "@envoy_api//bazel:api_build_system.bzl",
    "api_proto_library",
)

api_proto_library(
    name = "alts_socket_proto",
    srcs = [":alts_socket.proto"],
    visibility = ["//visibility:public"],
    require_py = 0,
)

envoy_cc_library(
    name = "grpc_tsi_wrapper",
    repository = "@envoy",
    visibility = ["//visibility:private"],
    hdrs = [
        "transport_security_interface_wrapper.h",
    ],
    external_deps = [
        "grpc",
    ],
)

envoy_cc_library(
    name = "tsi_handshaker",
    repository = "@envoy",
    visibility = ["//visibility:public"],
    srcs = [
        "tsi_handshaker.cc",
    ],
    hdrs = [
        "tsi_handshaker.h",
    ],
    deps = [
        ":grpc_tsi_wrapper",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

envoy_cc_library(
    name = "tsi_frame_protector",
    repository = "@envoy",
    visibility = ["//visibility:public"],
    srcs = [
        "tsi_frame_protector.cc",
    ],
    hdrs = [
        "tsi_frame_protector.h",
    ],
    deps = [
        ":grpc_tsi_wrapper",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

envoy_cc_library(
    name = "tsi_transport_socket",
    repository = "@envoy",
    visibility = ["//visibility:public"],
    srcs = [
        "tsi_transport_socket.cc",
    ],
    hdrs = [
        "tsi_transport_socket.h",
    ],
    deps = [
        ":tsi_frame_protector",
        ":tsi_handshaker",
        "@envoy//source/exe:envoy_common_lib",
    ],
)

envoy_cc_library(
    name = "alts_socket_factory",
    repository = "@envoy",
    visibility = ["//visibility:public"],
    srcs = [
        "alts_socket_factory.cc",
    ],
    hdrs = [
        "alts_socket_factory.h",
    ],
    deps = [
        ":grpc_tsi_wrapper",
        ":tsi_transport_socket",
        ":alts_socket_proto_cc",
        "@envoy//source/exe:envoy_common_lib",
    ],
)
