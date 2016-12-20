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
def boringssl_repositories(bind=True):
    # TODO(zlizan): Remove the custom BUILD file when
    # https://github.com/lyft/envoy/issues/301 resolved
    BUILD = """
# Copyright (c) 2016, Google Inc.
#
# Permission to use, copy, modify, and/or distribute this software for any
# purpose with or without fee is hereby granted, provided that the above
# copyright notice and this permission notice appear in all copies.
#
# THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
# WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
# MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY
# SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
# WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION
# OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF OR IN
# CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE. */

licenses(["notice"])

exports_files(["LICENSE"])

load(
    ":BUILD.generated.bzl",
    "crypto_headers",
    "crypto_internal_headers",
    "crypto_sources",
    "crypto_sources_linux_x86_64",
    "crypto_sources_mac_x86_64",
    "ssl_headers",
    "ssl_internal_headers",
    "ssl_sources",
    "tool_sources",
    "tool_headers",
)

config_setting(
    name = "linux_x86_64",
    values = {"cpu": "k8"},
)

config_setting(
    name = "mac_x86_64",
    values = {"cpu": "darwin"},
)

boringssl_copts = [
    # Assembler option --noexecstack adds .note.GNU-stack to each object to
    # ensure that binaries can be built with non-executable stack.
    "-Wa,--noexecstack",

    # This is needed on Linux systems (at least) to get rwlock in pthread.
    "-D_XOPEN_SOURCE=700",

    # This list of warnings should match those in the top-level CMakeLists.txt.
    "-Wall",
    "-Werror",
    "-Wformat=2",
    "-Wsign-compare",
    "-Wmissing-field-initializers",
    "-Wwrite-strings",
    "-Wshadow",
    "-fno-common",

    # Modern build environments should be able to set this to use atomic
    # operations for reference counting rather than locks. However, it's
    # known not to work on some Android builds.
    # "-DOPENSSL_C11_ATOMIC",
] + select({
    ":linux_x86_64": [],
    ":mac_x86_64": [],
    "//conditions:default": ["-DOPENSSL_NO_ASM"],
})

crypto_sources_asm = select({
    ":linux_x86_64": crypto_sources_linux_x86_64,
    ":mac_x86_64": crypto_sources_mac_x86_64,
    "//conditions:default": [],
})

# For C targets only (not C++), compile with C11 support.
boringssl_copts_c11 = boringssl_copts + [
    "-std=c11",
    "-Wmissing-prototypes",
    "-Wold-style-definition",
    "-Wstrict-prototypes",
]

# For C targets only (not C++), compile with C11 support.
boringssl_copts_cxx = boringssl_copts + [
    "-std=c++11",
    "-Wmissing-declarations",
]

cc_library(
    name = "crypto",
    srcs = crypto_sources + crypto_internal_headers + crypto_sources_asm,
    hdrs = crypto_headers,
    copts = boringssl_copts_c11,
    includes = ["src/include"],
    linkopts = select({
        ":mac_x86_64": [],
        "//conditions:default": ["-lpthread"],
    }),
    visibility = ["//visibility:public"],
)

cc_library(
    name = "ssl",
    srcs = ssl_sources + ssl_internal_headers,
    hdrs = ssl_headers,
    copts = boringssl_copts_c11,
    includes = ["src/include"],
    visibility = ["//visibility:public"],
    deps = [":crypto"],
)

cc_binary(
    name = "bssl",
    srcs = tool_sources + tool_headers,
    copts = boringssl_copts_cxx,
    visibility = ["//visibility:public"],
    deps = [":ssl"],
)

cc_library(
    name = "deprecit_base64_bio",
    srcs = [
        "src/decrepit/bio/base64_bio.c",
    ],
    copts = boringssl_copts_c11,
    includes = ["src/include"],
    visibility = ["//visibility:public"],
    deps = [":crypto"],
)"""

    native.new_git_repository(
        name = "boringssl",
        commit = "12c35d69008ae6b8486e435447445240509f7662",  # 2016-10-24
        remote = "https://boringssl.googlesource.com/boringssl",
        build_file_content = BUILD,
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
        commit = "a428e42072765993ff674fda72863c9f1aa2d268",  # v3.1.0
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
            actual = "@protobuf_git//:protobuf_lite",
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
