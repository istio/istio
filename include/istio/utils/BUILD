# Copyright 2018 Istio Authors. All Rights Reserved.
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
    name = "headers_lib",
    hdrs = [
        "attributes_builder.h",
        "md5.h",
        "protobuf.h",
        "status.h",
    ],
    visibility = ["//visibility:public"],
)

cc_library(
    name = "simple_lru_cache",
    srcs = ["google_macros.h"],
    hdrs = [
        "simple_lru_cache.h",
        "simple_lru_cache_inl.h",
    ],
    visibility = ["//visibility:public"],
)

cc_library(
    name = "attribute_names_header",
    hdrs = [
        "attribute_names.h",
    ],
    visibility = ["//visibility:public"],
)