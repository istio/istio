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
package(default_visibility = ["//visibility:private"])

load("@protobuf_git//:protobuf.bzl", "cc_proto_library")

cc_library(
    name = "transcoding_endpoints",
    srcs = [
        "transcoder_factory.cc",
        "transcoder_factory.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//contrib/endpoints/include:headers_only",
        "//external:service_config",
        "//external:transcoding",
    ],
)

cc_test(
    name = "transcoder_test",
    size = "small",
    srcs = [
        "transcoder_test.cc",
    ],
    data = [
        "@httpjson_transcoding//test:testdata/bookstore_service.pb.txt",
    ],
    deps = [
        ":transcoding_endpoints",
        "@httpjson_transcoding//test:bookstore_test_proto",
        "@httpjson_transcoding//test:test_common",
        "//external:googletest_main",
    ],
)
