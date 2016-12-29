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

cc_library(
    name = "api_manager",
    hdrs = [
        ":headers",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//contrib/endpoints/src/api_manager",
    ],
)

cc_library(
    name = "headers_only",
    hdrs = [
        ":headers",
    ],
    visibility = [
        "//contrib/endpoints/src:__subpackages__",
    ],
)

filegroup(
    name = "headers",
    srcs = [
        "api_manager/api_manager.h",
        "api_manager/compute_platform.h",
        "api_manager/env_interface.h",
        "api_manager/grpc_request.h",
        "api_manager/http_request.h",
        "api_manager/method.h",
        "api_manager/method_call_info.h",
        "api_manager/periodic_timer.h",
        "api_manager/protocol.h",
        "api_manager/request.h",
        "api_manager/request_handler_interface.h",
        "api_manager/response.h",
        "api_manager/service_control.h",
        "api_manager/utils/status.h",
        "api_manager/utils/version.h",
    ],
)
