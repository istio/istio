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
package(default_visibility = ["//src/api_manager:__subpackages__"])

cc_library(
    name = "cloud_trace",
    srcs = [
        "cloud_trace.cc",
        "cloud_trace.h",
    ],
    deps = [
        "//src/api_manager/auth:service_account_token",
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
        "//external:googletest_main",
        "//src/api_manager:mock_api_manager_environment",
        "//external:cloud_trace",
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
