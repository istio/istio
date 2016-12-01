# Copyright (C) Extensible Service Proxy Authors
# All rights reserved.
#
# Redistribution and use in source and binary forms, with or without
# modification, are permitted provided that the following conditions
# are met:
# 1. Redistributions of source code must retain the above copyright
#    notice, this list of conditions and the following disclaimer.
# 2. Redistributions in binary form must reproduce the above copyright
#    notice, this list of conditions and the following disclaimer in the
#    documentation and/or other materials provided with the distribution.
#
# THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
# ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
# IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
# ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
# FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
# DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
# OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
# HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
# LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
# OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
# SUCH DAMAGE.
#
################################################################################
#
exports_files(["version"])

cc_inc_library(
    name = "api_manager",
    hdrs = [
        ":headers",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//src/api_manager",
    ],
)

cc_library(
    name = "headers_only",
    hdrs = [
        ":headers",
    ],
    visibility = [
        "//src/api_manager:__subpackages__",
        "//src/grpc:__subpackages__",
    ],
)

filegroup(
    name = "headers",
    srcs = [
        "api_manager/api_manager.h",
        "api_manager/compute_platform.h",
        "api_manager/env_interface.h",
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
        "api_manager/version.h",
    ],
)

genrule(
    name = "api_manager_version_header",
    srcs = [
        ":version",
    ],
    outs = [
        "api_manager/version.h",
    ],
    cmd = "$(location //tools:create_version) $(location :version) > $@",
    tools = [
        "//tools:create_version",
    ],
)
