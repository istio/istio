# Copyright (C) Extensible Service Proxy Authors
# All rights reserved.
#
# Redistribution and use in source and binary forms, with or without
# modification, are permitted provided that the following conditions
# are met:
# 1. Redistributions of source code must retain the above copyright
#    notice, this list of conditions and the following disclaimer.
# 2. Redistributions in binary form must reproduce the above copyright
#    notice, this list of conditions and the following disclaimer in the
#    documentation and/or other materials provided with the distribution.
#
# THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
# ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
# IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
# ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
# FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
# DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
# OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
# HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
# LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
# OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
# SUCH DAMAGE.
#
################################################################################
#
package(default_visibility = ["//visibility:private"])

load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

cc_library(
    name = "prefix_writer",
    srcs = [
        "prefix_writer.cc",
    ],
    hdrs = [
        "prefix_writer.h",
    ],
    deps = [
        "//external:protobuf",
    ],
)

cc_library(
    name = "request_weaver",
    srcs = [
        "request_weaver.cc",
    ],
    hdrs = [
        "request_weaver.h",
    ],
    deps = [
        "//external:protobuf",
    ],
)

cc_library(
    name = "type_helper",
    srcs = [
        "type_helper.cc",
    ],
    hdrs = [
        "type_helper.h",
    ],
    deps = [
        "//external:protobuf",
        "//contrib/endpoints/include:headers_only",
    ],
)

cc_library(
    name = "message_stream",
    srcs = [
        "message_stream.cc",
    ],
    hdrs = [
        "message_stream.h",
    ],
    deps = [
        "//external:protobuf",
    ],
)

cc_library(
    name = "request_message_translator",
    srcs = [
        "request_message_translator.cc",
    ],
    hdrs = [
        "request_message_translator.h",
    ],
    deps = [
        ":message_stream",
        ":prefix_writer",
        ":request_weaver",
        "//external:protobuf",
    ],
)

cc_library(
    name = "request_stream_translator",
    srcs = [
        "request_stream_translator.cc",
    ],
    hdrs = [
        "request_stream_translator.h",
    ],
    deps = [
        ":request_message_translator",
        "//external:protobuf",
    ],
)

cc_library(
    name = "json_request_translator",
    srcs = [
        "json_request_translator.cc",
    ],
    hdrs = [
        "json_request_translator.h",
    ],
    deps = [
        ":request_message_translator",
        ":request_stream_translator",
        "//external:protobuf",
    ],
)

cc_library(
    name = "message_reader",
    srcs = [
        "message_reader.cc",
    ],
    hdrs = [
        "message_reader.h",
    ],
    deps = [
        "//external:protobuf",
    ],
)

cc_library(
    name = "response_to_json_translator",
    srcs = [
        "response_to_json_translator.cc",
    ],
    hdrs = [
        "response_to_json_translator.h",
    ],
    deps = [
        ":message_reader",
        ":message_stream",
        "//external:protobuf",
    ],
)

cc_library(
    name = "transcoding",
    srcs = [
        "transcoder_factory.cc",
    ],
    hdrs = [
        "transcoder_factory.h",
        "transcoder.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        ":json_request_translator",
        ":message_stream",
        ":response_to_json_translator",
        ":type_helper",
        "//external:protobuf",
        "//contrib/endpoints/include:headers_only",
        "//external:service_config",
    ],
)

cc_test(
    name = "prefix_writer_test",
    size = "small",
    srcs = [
        "prefix_writer_test.cc",
    ],
    deps = [
        ":prefix_writer",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "request_weaver_test",
    size = "small",
    srcs = [
        "request_weaver_test.cc",
    ],
    deps = [
        ":request_weaver",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "type_helper_test",
    size = "small",
    srcs = [
        "type_helper_test.cc",
    ],
    data = [
        "testdata/bookstore_service.pb.txt",
    ],
    deps = [
        ":test_common",
        ":type_helper",
        "//external:googletest_main",
        "//external:service_config",
    ],
)

cc_proto_library(
    name = "bookstore_test_proto",
    testonly = 1,
    srcs = ["bookstore.proto"],
    default_runtime = "//external:protobuf",
    protoc = "//external:protoc",
    deps = [
        "//external:cc_wkt_protos",
    ],
)

cc_library(
    name = "test_common",
    testonly = 1,
    srcs = ["test_common.cc"],
    hdrs = ["test_common.h"],
    deps = [
        "//external:googletest",
        "//external:protobuf",
        "//external:service_config",
    ],
)

cc_library(
    name = "request_translator_test_base",
    testonly = 1,
    srcs = [
        "proto_stream_tester.cc",
        "proto_stream_tester.h",
        "request_translator_test_base.cc",
    ],
    hdrs = [
        "request_translator_test_base.h",
    ],
    deps = [
        ":bookstore_test_proto",
        ":request_message_translator",
        ":test_common",
        ":type_helper",
        "//external:googletest",
        "//external:protobuf",
        "//external:service_config",
    ],
)

cc_test(
    name = "request_message_translator_test",
    size = "small",
    srcs = [
        "request_message_translator_test.cc",
    ],
    data = [
        "testdata/bookstore_service.pb.txt",
    ],
    deps = [
        ":bookstore_test_proto",
        ":request_message_translator",
        ":request_translator_test_base",
        ":test_common",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "request_stream_translator_test",
    size = "small",
    srcs = [
        "request_stream_translator_test.cc",
    ],
    data = [
        "testdata/bookstore_service.pb.txt",
    ],
    deps = [
        ":bookstore_test_proto",
        ":request_stream_translator",
        ":request_translator_test_base",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "json_request_translator_test",
    size = "small",
    srcs = [
        "json_request_translator_test.cc",
    ],
    data = [
        "testdata/bookstore_service.pb.txt",
    ],
    deps = [
        ":bookstore_test_proto",
        ":json_request_translator",
        ":request_translator_test_base",
        ":test_common",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "message_reader_test",
    size = "small",
    srcs = [
        "message_reader_test.cc",
    ],
    deps = [
        ":message_reader",
        ":test_common",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "response_to_json_translator_test",
    size = "small",
    srcs = [
        "response_to_json_translator_test.cc",
    ],
    data = [
        "testdata/bookstore_service.pb.txt",
    ],
    deps = [
        ":bookstore_test_proto",
        ":message_reader",
        ":response_to_json_translator",
        ":test_common",
        ":type_helper",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "message_stream_test",
    size = "small",
    srcs = [
        "message_stream_test.cc",
    ],
    deps = [
        ":message_stream",
        ":test_common",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "transcoder_test",
    size = "small",
    srcs = [
        "transcoder_test.cc",
    ],
    data = [
        "testdata/bookstore_service.pb.txt",
    ],
    deps = [
        ":bookstore_test_proto",
        ":test_common",
        ":transcoding",
        "//external:googletest_main",
    ],
)
