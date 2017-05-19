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
def boringssl_repositories(bind=True):
    native.git_repository(
        name = "boringssl",
        commit = "16efcb2dba4690b9940d9b95ef703f1bbd07494d",  # 2017-03-20
        remote = "https://boringssl.googlesource.com/boringssl",
    )

    if bind:
        native.bind(
            name = "boringssl_crypto",
            actual = "@boringssl//:crypto",
        )

        native.bind(
            name = "libssl",
            actual = "@boringssl//:ssl",
        )


def protobuf_repositories(bind=True):
    native.git_repository(
        name = "protobuf_git",
        commit = "593e917c176b5bc5aafa57bf9f6030d749d91cd5",  # v3.2.0
        remote = "https://github.com/google/protobuf.git",
    )

    if bind:
        native.bind(
            name = "protoc",
            actual = "@protobuf_git//:protoc",
        )

        native.bind(
            name = "protobuf",
            actual = "@protobuf_git//:protobuf",
        )

        native.bind(
            name = "cc_wkt_protos",
            actual = "@protobuf_git//:cc_wkt_protos",
        )

        native.bind(
            name = "cc_wkt_protos_genproto",
            actual = "@protobuf_git//:cc_wkt_protos_genproto",
        )

        native.bind(
            name = "protobuf_compiler",
            actual = "@protobuf_git//:protoc_lib",
        )

        native.bind(
            name = "protobuf_clib",
            actual = "@protobuf_git//:protoc_lib",
        )


def googletest_repositories(bind=True):
    BUILD = """
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
    name = "googletest",
    srcs = [
        "googletest/src/gtest-all.cc",
        "googlemock/src/gmock-all.cc",
    ],
    hdrs = glob([
        "googletest/include/**/*.h",
        "googlemock/include/**/*.h",
        "googletest/src/*.cc",
        "googletest/src/*.h",
        "googlemock/src/*.cc",
    ]),
    includes = [
        "googlemock",
        "googletest",
        "googletest/include",
        "googlemock/include",
    ],
    visibility = ["//visibility:public"],
)

cc_library(
    name = "googletest_main",
    srcs = ["googlemock/src/gmock_main.cc"],
    visibility = ["//visibility:public"],
    linkopts = [
        "-lpthread",
    ],
    deps = [":googletest"],
)

cc_library(
    name = "googletest_prod",
    hdrs = [
        "googletest/include/gtest/gtest_prod.h",
    ],
    includes = [
        "googletest/include",
    ],
    visibility = ["//visibility:public"],
)
"""
    native.new_git_repository(
        name = "googletest_git",
        build_file_content = BUILD,
        commit = "d225acc90bc3a8c420a9bcd1f033033c1ccd7fe0",
        remote = "https://github.com/google/googletest.git",
    )

    if bind:
        native.bind(
            name = "googletest",
            actual = "@googletest_git//:googletest",
        )

        native.bind(
            name = "googletest_main",
            actual = "@googletest_git//:googletest_main",
        )

        native.bind(
            name = "googletest_prod",
            actual = "@googletest_git//:googletest_prod",
        )

def transcoding_repositories(bind=True):
    native.git_repository(
        name = "httpjson_transcoding",
        commit = "d91a0fae8f9248c6b317abfa55b773fd62873818",
        remote = "https://github.com/grpc-ecosystem/grpc-httpjson-transcoding.git",
    )

    if bind:
        native.bind(
            name = "transcoding",
            actual = "@httpjson_transcoding//src:transcoding",
        )

        native.bind(
            name = "path_matcher",
            actual = "@httpjson_transcoding//src:path_matcher",
        )

def zlib_repositories(bind=True):
    BUILD = """
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

licenses(["notice"])

exports_files(["README"])

cc_library(
    name = "zlib",
    srcs = [
        "adler32.c",
        "crc32.c",
        "crc32.h",
        "deflate.c",
        "deflate.h",
        "infback.c",
        "inffast.c",
        "inffast.h",
        "inffixed.h",
        "inflate.c",
        "inflate.h",
        "inftrees.c",
        "inftrees.h",
        "trees.c",
        "trees.h",
        "zconf.h",
        "zutil.c",
        "zutil.h",
    ],
    hdrs = [
        "zlib.h",
    ],
    copts = [
        "-Wno-shift-negative-value",
        "-Wno-unknown-warning-option",
    ],
    defines = [
        "Z_SOLO",
    ],
    visibility = [
        "//visibility:public",
    ],
)
"""

    native.new_git_repository(
        name = "zlib_git",
        build_file_content = BUILD,
        commit = "50893291621658f355bc5b4d450a8d06a563053d",  # v1.2.8
        remote = "https://github.com/madler/zlib.git",
    )

    native.bind(
        name = "zlib",
        actual = "@zlib_git//:zlib"
    )


def grpc_repositories(bind=True, envoy_deps=False):
    zlib_repositories(bind)

    native.git_repository(
        name = "grpc_git",
        commit = "c80d3321d0f77bef8cfff8b32490a07c1e90a5ad", # v1.3.2
        remote = "https://github.com/grpc/grpc.git",
    )

    native.bind(
        name = "nanopb",
        actual = "@grpc_git//third_party/nanopb",
    )

    if envoy_deps:
        native.bind(
            name = "cares",
            actual = "@envoy_deps//:ares",
        )

    if bind:
        native.bind(
            name = "gpr",
            actual = "@grpc_git//:gpr",
        )

        native.bind(
            name = "grpc",
            actual = "@grpc_git//:grpc",
        )

        native.bind(
            name = "grpc_cpp_plugin",
            actual = "@grpc_git//:grpc_cpp_plugin",
        )

        native.bind(
            name = "grpc++",
            actual = "@grpc_git//:grpc++",
        )

        native.bind(
            name = "grpc_lib",
            actual = "@grpc_git//:grpc++_codegen_proto",
        )

def googleapis_repositories(protobuf_repo="@protobuf_git//", bind=True):
    BUILD = """
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

licenses(["notice"])

load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

exports_files(glob(["google/**"]))

cc_proto_library(
    name = "annotations",
    srcs = [
        "google/api/annotations.proto",
        "google/api/http.proto",
    ],
    include = ".",
    visibility = ["//visibility:public"],
    deps = [
        "//external:cc_wkt_protos",
    ],
    protoc = "//external:protoc",
    default_runtime = "//external:protobuf",
)

cc_proto_library(
    name = "servicecontrol",
    srcs = [
        "google/api/servicecontrol/v1/check_error.proto",
        "google/api/servicecontrol/v1/distribution.proto",
        "google/api/servicecontrol/v1/log_entry.proto",
        "google/api/servicecontrol/v1/metric_value.proto",
        "google/api/servicecontrol/v1/operation.proto",
        "google/api/servicecontrol/v1/service_controller.proto",
        "google/api/servicemanagement/v1/servicemanager.proto",
        "google/api/servicemanagement/v1/resources.proto",
        "google/logging/type/http_request.proto",
        "google/logging/type/log_severity.proto",
        "google/api/config_change.proto",
        "google/longrunning/operations.proto",
        "google/rpc/error_details.proto",
        "google/rpc/status.proto",
        "google/type/money.proto",
    ],
    include = ".",
    visibility = ["//visibility:public"],
    deps = [
        ":service_config",
    ],
    protoc = "//external:protoc",
    default_runtime = "//external:protobuf",
)

cc_proto_library(
    name = "service_config",
    srcs = [
        "google/api/auth.proto",
        "google/api/backend.proto",
        "google/api/billing.proto",
        "google/api/consumer.proto",
        "google/api/context.proto",
        "google/api/control.proto",
        "google/api/documentation.proto",
        "google/api/endpoint.proto",
        "google/api/label.proto",
        "google/api/log.proto",
        "google/api/logging.proto",
        "google/api/metric.proto",
        "google/api/experimental/experimental.proto",
        "google/api/experimental/authorization_config.proto",
        "google/api/monitored_resource.proto",
        "google/api/monitoring.proto",
        "google/api/quota.proto",
        "google/api/service.proto",
        "google/api/source_info.proto",
        "google/api/system_parameter.proto",
        "google/api/usage.proto",
    ],
    include = ".",
    visibility = ["//visibility:public"],
    deps = [
        ":annotations",
        "//external:cc_wkt_protos",
    ],
    protoc = "//external:protoc",
    default_runtime = "//external:protobuf",
)

cc_proto_library(
    name = "cloud_trace",
    srcs = [
        "google/devtools/cloudtrace/v1/trace.proto",
    ],
    include = ".",
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    visibility = ["//visibility:public"],
    deps = [
        ":service_config",
        "//external:cc_wkt_protos",
    ],
)
""".format(protobuf_repo)

    native.new_git_repository(
        name = "googleapis_git",
        commit = "a25bc13463f6fde03d78665a9a7042c103b7e65b",
        remote = "https://github.com/googleapis/googleapis.git",
        build_file_content = BUILD,
    )

    if bind:
        native.bind(
            name = "servicecontrol",
            actual = "@googleapis_git//:servicecontrol",
        )

        native.bind(
            name = "servicecontrol_genproto",
            actual = "@googleapis_git//:servicecontrol_genproto",
        )

        native.bind(
            name = "service_config",
            actual = "@googleapis_git//:service_config",
        )

        native.bind(
            name = "cloud_trace",
            actual = "@googleapis_git//:cloud_trace",
        )
