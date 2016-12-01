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
package(default_visibility = ["//src/api_manager:__subpackages__"])

cc_library(
    name = "context",
    srcs = [
        "request_context.cc",
        "service_context.cc",
    ],
    hdrs = [
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
        "//external:cc_wkt_protos",
        "//external:googletest_prod",
        "//external:grpc++",
        "//external:protobuf",
        "//src/api_manager:impl_headers",
        "//src/api_manager:server_config_proto",
        "//src/api_manager/auth",
        "//src/api_manager/auth:service_account_token",
        "//src/api_manager/cloud_trace",
        "//src/api_manager/service_control",
        "//src/api_manager/utils",
        "//external:cloud_trace",
        "//external:service_config",
        "//external:servicecontrol",
        "//third_party/service-control-client-cxx:service_control_client_lib",
    ],
)
