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
    name = "common_lib",
    srcs = [
        "attribute_names.cc",
        "client_context_base.cc",
    ],
    hdrs = [
        "attribute_names.h",
        "client_context_base.h",
        "request_context.h",
    ],
    visibility = [":__subpackages__"],
    deps = [
        "//:mixer_client_lib",
        "@mixerapi_git//:mixer_client_config_cc_proto",
    ],
)

cc_library(
    name = "mock_mixer_client",
    hdrs = [
        "mock_mixer_client.h",
    ],
    visibility = [":__subpackages__"],
    deps = [
        "//:mixer_client_lib",
    ],
)
