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

ISTIO_API_SHA = "df00c61b5b38b3b6df2ea820bf15d232247c8ff4"

def go_istio_api_repositories(use_local=False):
    ISTIO_API_BUILD_FILE = """
# build protos from istio.io/api repo

package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_library")

go_prefix("istio.io/api")

# load("@io_bazel_rules_go//proto:go_proto_library.bzl", "go_proto_library")
load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library", "gogo_proto_compile")
load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "broker/v1/config",
    protos = glob(["broker/v1/config/*.proto"]),
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
)

go_proto_library(
    name = "proxy/v1/config",
    imports = [
        "../../external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = glob(["proxy/v1/config/*.proto"]),
    verbose = 0,
    deps = [
        "@com_github_golang_protobuf//ptypes/any:go_default_library",
        "@com_github_golang_protobuf//ptypes/duration:go_default_library",
        "@com_github_golang_protobuf//ptypes/wrappers:go_default_library",
    ],
)

gogoslick_proto_library(
    name = "mixer/v1",
    importmap = {
        "gogoproto/gogo.proto": "github.com/gogo/protobuf/gogoproto",
        "google/rpc/status.proto": "github.com/googleapis/googleapis/google/rpc",
        "google/protobuf/timestamp.proto": "github.com/gogo/protobuf/types",
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "../../external/com_github_gogo_protobuf",
        "../../external/com_github_google_protobuf/src",
        "../../external/com_github_googleapis_googleapis",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
        "@com_github_googleapis_googleapis//:status_proto",
        "@com_github_gogo_protobuf//gogoproto:go_default_library_protos",
    ],
    protos = [
        "mixer/v1/attributes.proto",
        "mixer/v1/check.proto",
        "mixer/v1/report.proto",
        "mixer/v1/service.proto",
    ],
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = True,
    deps = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

DESCRIPTOR_FILE_GROUP = [
    "mixer/v1/config/descriptor/log_entry_descriptor.proto",
    "mixer/v1/config/descriptor/metric_descriptor.proto",
    "mixer/v1/config/descriptor/monitored_resource_descriptor.proto",
    "mixer/v1/config/descriptor/principal_descriptor.proto",
    "mixer/v1/config/descriptor/quota_descriptor.proto",
    "mixer/v1/config/descriptor/value_type.proto",
]

# gogoslick_proto_compile cannot be used here. it generates Equal, Size, and
# MarshalTo methods for google.protobuf.Struct, which we then later replace
# with interface{}. This causes compilation issues.
gogo_proto_compile(
    name = "mixer/v1/config_gen",
    importmap = {
        "google/protobuf/struct.proto": "github.com/gogo/protobuf/types",
        "mixer/v1/config/descriptor/log_entry_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/metric_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/monitored_resource_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/principal_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/quota_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/value_type.proto": "istio.io/api/mixer/v1/config/descriptor",
    },
    imports = [
        "../../external/com_github_google_protobuf/src",
    ],
    inputs = DESCRIPTOR_FILE_GROUP + [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "mixer/v1/config/cfg.proto",
    ],
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = False,
)


gogoslick_proto_library(
    name = "mixer/v1/template",
    importmap = {
        "google/protobuf/descriptor.proto": "github.com/gogo/protobuf/protoc-gen-gogo/descriptor",
    },
    imports = [
        "../../external/com_github_google_protobuf/src",
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = ["mixer/v1/template/extensions.proto"],
    verbose = 0,
    with_grpc = False,
    deps = [
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_gogo_protobuf//protoc-gen-gogo/descriptor:go_default_library",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)

filegroup(
    name = "mixer/v1/template_protos",
    srcs = ["mixer/v1/template/extensions.proto"],
    visibility = ["//visibility:public"],
)

gogoslick_proto_library(
    name = "mixer/v1/config/descriptor",
    importmap = {
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "../../external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = DESCRIPTOR_FILE_GROUP,
    verbose = 0,
    visibility = ["//visibility:public"],
    with_grpc = False,
    deps = [
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)

filegroup(
    name = "mixer/v1/config/descriptor_protos",
    srcs = DESCRIPTOR_FILE_GROUP,
    visibility = ["//visibility:public"],
)

genrule(
    name = "mixer/v1/config_fixed",
    srcs = [":mixer/v1/config_gen"],
    outs = ["fixed_cfg.pb.go"],
    cmd = "sed " +
          "-e 's/*google_protobuf.Struct/interface{}/g' " +
          "-e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' " +
          "$(location :mixer/v1/config_gen) | $(location @org_golang_x_tools_imports//:goimports) > $@",
    message = "Applying overrides to cfg proto",
    tools = ["@org_golang_x_tools_imports//:goimports"],
)

filegroup(
    name = "mixer/v1/attributes_file",
    srcs = ["mixer/v1/global_dictionary.yaml"],
    visibility = ["//visibility:public"],
)
"""
    if use_local:
        native.new_local_repository(
            name = "io_istio_api",
            build_file_content = ISTIO_API_BUILD_FILE,
            path = "../api",
        )
    else:
      native.new_git_repository(
          name = "io_istio_api",
          build_file_content = ISTIO_API_BUILD_FILE,
          commit = ISTIO_API_SHA,
          remote = "https://github.com/istio/api.git",
      )
