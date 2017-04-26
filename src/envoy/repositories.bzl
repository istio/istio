# Copyright 2016 Istio Authors. All Rights Reserved.
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
def lightstep_repositories(bind=True):
    BUILD = """
load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

cc_library(
    name = "lightstep_core",
    srcs = [
        "src/c++11/impl.cc",
        "src/c++11/span.cc",
        "src/c++11/tracer.cc",
        "src/c++11/util.cc",
    ],
    hdrs = [
        "src/c++11/lightstep/impl.h",
        "src/c++11/lightstep/options.h",
        "src/c++11/lightstep/propagation.h",
        "src/c++11/lightstep/carrier.h",
        "src/c++11/lightstep/span.h",
        "src/c++11/lightstep/tracer.h",
        "src/c++11/lightstep/util.h",
        "src/c++11/lightstep/value.h",
        "src/c++11/mapbox_variant/recursive_wrapper.hpp",
        "src/c++11/mapbox_variant/variant.hpp",
    ],
    copts = [
        "-DPACKAGE_VERSION='\\"0.36\\"'",
        "-Iexternal/lightstep_git/src/c++11/lightstep",
        "-Iexternal/lightstep_git/src/c++11/mapbox_variant",
    ],
    includes = [
        "src/c++11",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@lightstep_common_git//:collector_proto",
        "@lightstep_common_git//:lightstep_carrier_proto",
        "//external:protobuf",
    ],
)"""

    COMMON_BUILD = """
load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

cc_proto_library(
    name = "collector_proto",
    srcs = ["collector.proto"],
    include = ".",
    deps = [
        "//external:cc_wkt_protos",
    ],
    protoc = "//external:protoc",
    default_runtime = "//external:protobuf",
    visibility = ["//visibility:public"],
)

cc_proto_library(
    name = "lightstep_carrier_proto",
    srcs = ["lightstep_carrier.proto"],
    include = ".",
    deps = [
        "//external:cc_wkt_protos",
    ],
    protoc = "//external:protoc",
    default_runtime = "//external:protobuf",
    visibility = ["//visibility:public"],
)
"""

    native.new_git_repository(
        name = "lightstep_common_git",
        remote = "https://github.com/lightstep/lightstep-tracer-common.git",
        commit = "cbbecd671c1ae1f20ae873c5da688c8c14d04ec3",
        build_file_content = COMMON_BUILD,
    )

    native.new_git_repository(
        name = "lightstep_git",
        remote = "https://github.com/lightstep/lightstep-tracer-cpp.git",
        commit = "f1dc8f3dfd529350e053fd21273e627f409ae428", # 0.36
        build_file_content = BUILD,
    )

    if bind:
        native.bind(
            name = "lightstep",
            actual = "@lightstep_git//:lightstep_core",
        )

