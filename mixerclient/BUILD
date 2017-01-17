# Copyright 2017 Google Inc. All Rights Reserved.
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
    "src/client_impl.h",
    "src/client_impl.cc",
 ],
 hdrs = [
     "include/client.h",
     "include/options.h",
 ],
 visibility = ["//visibility:public"],
 deps = [
     "//external:boringssl_crypto",
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
    name = "mixer_client_impl_test",
    size = "small",
    srcs = ["src/client_impl_test.cc"],
    linkopts = ["-lm"],
    deps = [
        ":mixer_client_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "simple_lru_cache_test",
    size = "small",
    srcs = ["utils/simple_lru_cache_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    deps = [
        ":simple_lru_cache",
        "//external:googletest_main",
    ],
)