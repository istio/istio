# Copyright 2017 Istio Authors. All Rights Reserved.
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

load("@io_bazel_rules_go//go:def.bzl", "go_library")
load(":test_suite.bzl", "go_test_suite")

go_library(
    name = "go_default_library",
    srcs = [
        "attributes.go",
        "envoy.go",
        "envoy_conf.go",
        "http_client.go",
        "http_server.go",
        "mixer_server.go",
        "setup.go",
    ],
    deps = [
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
        "@com_github_istio_api//:mixer/v1",
        "@com_github_istio_api//:mixer/v1/config",
        "@com_github_istio_mixer//pkg/adapterManager:go_default_library",
        "@com_github_istio_mixer//pkg/api:go_default_library",
        "@com_github_istio_mixer//pkg/aspect:go_default_library",
        "@com_github_istio_mixer//pkg/attribute:go_default_library",
        "@com_github_istio_mixer//pkg/pool:go_default_library",
        "@com_github_istio_mixer//pkg/status:go_default_library",
        "@com_github_istio_mixer//pkg/tracing:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//codes:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_google_grpc//stats:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

go_test_suite(
    data = [
        "//src/envoy/mixer:envoy",
    ],
    library = ":go_default_library",
    tags = [
        # Use fixed ports, not in sanbbox, have to be run exclusively.
        "exclusive",
        # shared memory path /envoy_shared_memory_0 used by Envoy
        # hot start is not working in sandbox mode.
        "local",
    ],
    tests = [
        "check_cache_test.go",
        "check_fail_close_test.go",
        "check_fail_open_test.go",
        "check_report_test.go",
        "failed_request_test.go",
        "quota_cache_test.go",
        "quota_test.go",
        "report_batch_test.go",
        "stress_test.go",
        "tcp_filter_test.go",
    ],
)
