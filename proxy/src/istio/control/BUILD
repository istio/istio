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
    name = "common_lib",
    srcs = [
        "client_context_base.cc",
    ],
    hdrs = [
        "client_context_base.h",
        "request_context.h",
    ],
    visibility = [":__subpackages__"],
    deps = [
        "//external:mixer_client_config_cc_proto",
        "//proxy/include/istio/utils:attribute_names_header",
        "//proxy/src/istio/mixerclient:mixerclient_lib",
        "//proxy/src/istio/quota_config:config_parser_lib",
        "//proxy/src/istio/utils:attribute_names_lib",
    ],
)

cc_library(
    name = "mock_mixer_client",
    hdrs = [
        "mock_mixer_client.h",
    ],
    visibility = [":__subpackages__"],
    deps = [
        "//proxy/src/istio/mixerclient:mixerclient_lib",
    ],
)
