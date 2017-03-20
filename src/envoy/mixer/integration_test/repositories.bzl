load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "new_go_repository", "go_repository")
load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_repositories")

load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogo_proto_repositories")
load("@org_pubref_rules_protobuf//cpp:rules.bzl", "cpp_proto_repositories")

def go_istio_api_repositories(use_local=False):
    ISTIO_API_BUILD_FILE = """
# build protos from istio.io/api repo

package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix")

go_prefix("istio.io/api")

load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

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
        "mixer/v1/quota.proto",
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
    "mixer/v1/config/descriptor/attribute_descriptor.proto",
    "mixer/v1/config/descriptor/label_descriptor.proto",
    "mixer/v1/config/descriptor/log_entry_descriptor.proto",
    "mixer/v1/config/descriptor/metric_descriptor.proto",
    "mixer/v1/config/descriptor/monitored_resource_descriptor.proto",
    "mixer/v1/config/descriptor/principal_descriptor.proto",
    "mixer/v1/config/descriptor/quota_descriptor.proto",
    "mixer/v1/config/descriptor/value_type.proto",
]

gogoslick_proto_library(
    name = "mixer/v1/config",
    importmap = {
        "google/protobuf/struct.proto": "github.com/gogo/protobuf/types",
        "mixer/v1/config/descriptor/log_entry_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/metric_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/monitored_resource_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/principal_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
        "mixer/v1/config/descriptor/quota_descriptor.proto": "istio.io/api/mixer/v1/config/descriptor",
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
    deps = [
        ":mixer/v1/config/descriptor",
        "@com_github_gogo_protobuf//sortkeys:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
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
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)
"""
    if use_local:
        native.new_local_repository(
            name = "com_github_istio_api",
            build_file_content = ISTIO_API_BUILD_FILE,
            path = "../api",
        )
    else:
      native.new_git_repository(
          name = "com_github_istio_api",
          build_file_content = ISTIO_API_BUILD_FILE,
          commit = "2cb09827d7f09a6e88eac2c2249dcb45c5419f09", # Mar. 14, 2017 (no releases)
          remote = "https://github.com/istio/api.git",
      )

def go_googleapis_repositories():
    GOOGLEAPIS_BUILD_FILE = """
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix")
go_prefix("github.com/googleapis/googleapis")

load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "google/rpc",
    protos = [
        "google/rpc/code.proto",
        "google/rpc/error_details.proto",
        "google/rpc/status.proto",
    ],
    importmap = {
        "google/protobuf/any.proto": "github.com/gogo/protobuf/types",
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "../../external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    deps = [
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
    verbose = 0,
)

load("@org_pubref_rules_protobuf//cpp:rules.bzl", "cc_proto_library")

cc_proto_library(
    name = "cc_status_proto",
    protos = [
        "google/rpc/status.proto",
    ],
    imports = [
        "../../external/com_github_google_protobuf/src",
    ],
    verbose = 0,
)

filegroup(
    name = "status_proto",
    srcs = [ "google/rpc/status.proto" ],
)

filegroup(
    name = "code_proto",
    srcs = [ "google/rpc/code.proto" ],
)
"""
    native.new_git_repository(
        name = "com_github_googleapis_googleapis",
        build_file_content = GOOGLEAPIS_BUILD_FILE,
        commit = "13ac2436c5e3d568bd0e938f6ed58b77a48aba15", # Oct 21, 2016 (only release pre-dates sha)
        remote = "https://github.com/googleapis/googleapis.git",
    )

def go_mixer_repositories(use_local_api=False):
    go_istio_api_repositories(use_local_api)
    go_googleapis_repositories()

    go_repositories()
    proto_repositories()

    gogo_proto_repositories()

    new_go_repository(
        name = "com_github_golang_glog",
        commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998", # Jan 26, 2016 (no releases)
        importpath = "github.com/golang/glog",
    )

    new_go_repository(
        name = "com_github_ghodss_yaml",
        commit = "04f313413ffd65ce25f2541bfd2b2ceec5c0908c", # Dec 6, 2016 (no releases)
        importpath = "github.com/ghodss/yaml",
    )

    new_go_repository(
        name = "in_gopkg_yaml_v2",
        commit = "14227de293ca979cf205cd88769fe71ed96a97e2", # Jan 24, 2017 (no releases)
        importpath = "gopkg.in/yaml.v2",
    )

    new_go_repository(
        name = "com_github_golang_protobuf",
        commit = "8ee79997227bf9b34611aee7946ae64735e6fd93", # Nov 16, 2016 (no releases)
        importpath = "github.com/golang/protobuf",
    )

    new_go_repository(
        name = "org_golang_google_grpc",
        commit = "708a7f9f3283aa2d4f6132d287d78683babe55c8", # Dec 5, 2016 (v1.0.5)
        importpath = "google.golang.org/grpc",
    )

    new_go_repository(
        name = "com_github_spf13_cobra",
        commit = "35136c09d8da66b901337c6e86fd8e88a1a255bd", # Jan 30, 2017 (no releases)
        importpath = "github.com/spf13/cobra",
    )

    new_go_repository(
        name = "com_github_spf13_pflag",
        commit = "9ff6c6923cfffbcd502984b8e0c80539a94968b7", # Jan 30, 2017 (no releases)
        importpath = "github.com/spf13/pflag",
    )

    new_go_repository(
        name = "com_github_hashicorp_go_multierror",
        commit = "ed905158d87462226a13fe39ddf685ea65f1c11f", # Dec 16, 2016 (no releases)
        importpath = "github.com/hashicorp/go-multierror",
    )

    new_go_repository(
        name = "com_github_hashicorp_errwrap",
        commit = "7554cd9344cec97297fa6649b055a8c98c2a1e55", # Oct 27, 2014 (no releases)
        importpath = "github.com/hashicorp/errwrap",
    )

    new_go_repository(
        name = "com_github_opentracing_opentracing_go",
        commit = "0c3154a3c2ce79d3271985848659870599dfb77c", # Sep 26, 2016 (v1.0.0)
        importpath = "github.com/opentracing/opentracing-go",
    )

    new_go_repository(
        name = "com_github_opentracing_basictracer",
        commit = "1b32af207119a14b1b231d451df3ed04a72efebf", # Sep 29, 2016 (no releases)
        importpath = "github.com/opentracing/basictracer-go",
    )

    go_repository(
        name = "com_github_istio_mixer",
        commit = "064001053b51f73adc3a80ff87ef41a15316c300",
        importpath = "github.com/istio/mixer",
    )
    
