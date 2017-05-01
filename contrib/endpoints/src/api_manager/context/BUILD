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
    name = "context",
    srcs = [
        "global_context.cc",
        "request_context.cc",
        "service_context.cc",
    ],
    hdrs = [
        "global_context.h",
        "request_context.h",
        "service_context.h",
    ],
    linkopts = select({
        "//:darwin": [],
        "//conditions:default": [
            "-lm",
            "-luuid",
        ],
    }),
    deps = [
        "//contrib/endpoints/src/api_manager:http_template",
        "//contrib/endpoints/src/api_manager:impl_headers",
        "//contrib/endpoints/src/api_manager:path_matcher",
        "//contrib/endpoints/src/api_manager:server_config_proto",
        "//contrib/endpoints/src/api_manager/auth",
        "//contrib/endpoints/src/api_manager/auth:service_account_token",
        "//contrib/endpoints/src/api_manager/cloud_trace",
        "//contrib/endpoints/src/api_manager/service_control",
        "//contrib/endpoints/src/api_manager/utils",
        "//external:cc_wkt_protos",
        "//external:cloud_trace",
        "//external:googletest_prod",
        "//external:grpc++",
        "//external:protobuf",
        "//external:service_config",
        "//external:servicecontrol",
        "//external:servicecontrol_client",
    ],
)
