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
package(default_visibility = ["//contrib/endpoints/src/api_manager:__subpackages__"])

cc_library(
    name = "cloud_trace",
    srcs = [
        "cloud_trace.cc",
        "cloud_trace.h",
    ],
    deps = [
        "//contrib/endpoints/include:headers_only",
        "//contrib/endpoints/src/api_manager/auth:service_account_token",
        "//external:cloud_trace",
    ],
)

cc_test(
    name = "cloud_trace_test",
    size = "small",
    srcs = [
        "cloud_trace_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":cloud_trace",
        "//contrib/endpoints/src/api_manager:mock_api_manager_environment",
        "//external:cloud_trace",
        "//external:googletest_main",
    ],
)

cc_test(
    name = "sampler_test",
    size = "small",
    srcs = [
        "sampler_test.cc",
    ],
    linkstatic = 1,
    deps = [
        ":cloud_trace",
        "//external:googletest_main",
    ],
)
