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

cc_library(
    name = "quota_prefetch_lib",
    srcs = [
        "circular_queue.h",
        "quota_prefetch.cc",
        "time_based_counter.cc",
        "time_based_counter.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//proxy/include/istio/prefetch:headers_lib",
    ],
)

cc_test(
    name = "circular_queue_test",
    size = "small",
    srcs = ["circular_queue_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":quota_prefetch_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "time_based_counter_test",
    size = "small",
    srcs = ["time_based_counter_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":quota_prefetch_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "quota_prefetch_test",
    size = "small",
    srcs = ["quota_prefetch_test.cc"],
    linkopts = [
        "-lm",
        "-lpthread",
    ],
    linkstatic = 1,
    deps = [
        ":quota_prefetch_lib",
        "//external:googletest_main",
    ],
)
