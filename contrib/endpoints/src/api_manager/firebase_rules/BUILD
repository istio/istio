# Copyright 2017 Google Inc. All Rights Reserved.
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
package(default_visibility = ["//contrib/endpoints/src/api_manager:__subpackages__"])

cc_library(
    name = "firebase_rules",
    srcs = [
        "firebase_request.cc",
    ],
    hdrs = [
        "firebase_request.h",
    ],
    linkopts = select({
        "//:darwin": [],
        "//conditions:default": [
            "-lm",
            "-luuid",
        ],
    }),
    deps = [
        "//contrib/endpoints/src/api_manager:security_rules_proto",
        "//contrib/endpoints/src/api_manager/context",
        "//contrib/endpoints/src/api_manager/utils",
        "//external:cc_wkt_protos",
        "//external:googletest_prod",
    ],
)
