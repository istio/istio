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
        commit = "580b222e225076d0d7a6c65d472398a5f089fb51",  # istio branch
        remote = "https://github.com/istio/protobuf.git",
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
        commit = "193aa283914ba701c12cfdfa5967c1c4210468e3",
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
