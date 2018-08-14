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

py_binary(
    name = "create_global_dictionary",
    srcs = ["create_global_dictionary.py"],
)

genrule(
    name = "global_dictionary_header_gen",
    srcs = [
        "@mixerapi_git//:global_dictionary_file",
    ],
    outs = [
        "global_dictionary.cc",
    ],
    cmd = "$(location :create_global_dictionary) $(location @mixerapi_git//:global_dictionary_file) > $@",
    tools = [
        ":create_global_dictionary",
    ],
)

cc_library(
    name = "mixerclient_lib",
    srcs = [
        "attribute_compressor.cc",
        "attribute_compressor.h",
        "check_cache.cc",
        "check_cache.h",
        "client_impl.cc",
        "client_impl.h",
        "delta_update.cc",
        "delta_update.h",
        "global_dictionary.cc",
        "global_dictionary.h",
        "quota_cache.cc",
        "quota_cache.h",
        "referenced.cc",
        "referenced.h",
        "report_batch.cc",
        "report_batch.h",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//external:mixer_api_cc_proto",
        "//proxy/include/istio/mixerclient:headers_lib",
        "//proxy/include/istio/quota_config:requirement_header",
        "//proxy/include/istio/utils:simple_lru_cache",
        "//proxy/src/istio/prefetch:quota_prefetch_lib",
        "//proxy/src/istio/utils:md5_lib",
        "//proxy/src/istio/utils:utils_lib",
    ],
)

cc_library(
    name = "status_test_util_lib",
    hdrs = [
        "status_test_util.h",
    ],
    visibility = ["//visibility:public"],
)

cc_test(
    name = "attribute_compressor_test",
    size = "small",
    srcs = ["attribute_compressor_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixerclient_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "check_cache_test",
    size = "small",
    srcs = ["check_cache_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixerclient_lib",
        ":status_test_util_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "delta_update_test",
    size = "small",
    srcs = ["delta_update_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixerclient_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "report_batch_test",
    size = "small",
    srcs = ["report_batch_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixerclient_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "quota_cache_test",
    size = "small",
    srcs = ["quota_cache_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixerclient_lib",
        ":status_test_util_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "referenced_test",
    size = "small",
    srcs = ["referenced_test.cc"],
    linkstatic = 1,
    deps = [
        ":mixerclient_lib",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "client_impl_test",
    size = "small",
    srcs = ["client_impl_test.cc"],
    linkopts = select({
        "//proxy:darwin": [],
        "//conditions:default": [
            "-lm",
            "-lpthread",
            "-lrt",
        ],
    }),
    linkstatic = 1,
    deps = [
        ":mixerclient_lib",
        ":status_test_util_lib",
        "//external:googletest_main",
    ],
)
