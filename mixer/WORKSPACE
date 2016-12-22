workspace(name = "com_github_istio_mixer")

git_repository(
    name = "io_bazel_rules_go",
    commit = "2c6fa767100b95799043451aa3ee221d9a9bbb55",
    remote = "https://github.com/bazelbuild/rules_go.git",
)

load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "new_go_repository")

go_repositories()

git_repository(
    name = "org_pubref_rules_protobuf",
    commit = "c0013ac259444437f913e7dd0b10e36ce3325ed4",
    remote = "https://github.com/pubref/rules_protobuf",
)

load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_repositories")

proto_repositories()

load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_repositories")
load("@org_pubref_rules_protobuf//cpp:rules.bzl", "cpp_proto_repositories")

cpp_proto_repositories()

go_proto_repositories()

new_go_repository(
    name = "com_github_golang_glog",
    commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998",
    importpath = "github.com/golang/glog",
)

new_go_repository(
    name = "in_gopkg_yaml_v2",
    commit = "a5b47d31c556af34a302ce5d659e6fea44d90de0",
    importpath = "gopkg.in/yaml.v2",
)

new_go_repository(
    name = "com_github_golang_protobuf",
    commit = "8ee79997227bf9b34611aee7946ae64735e6fd93",
    importpath = "github.com/golang/protobuf",
)

GOOGLEAPIS_BUILD_FILE = """
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_prefix")
go_prefix("github.com/googleapis/googleapis")

load("@org_pubref_rules_protobuf//go:rules.bzl", "go_proto_library")

go_proto_library(
    name = "go_status_proto",
    protos = [
        "google/rpc/status.proto",
    ],
    imports = [
        "../../external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_google_protobuf//:well_known_protos",
    ],
    deps = [
        "@com_github_golang_protobuf//ptypes/any:go_default_library",
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
"""

new_git_repository(
    name = "com_github_googleapis_googleapis",
    build_file_content = GOOGLEAPIS_BUILD_FILE,
    commit = "13ac2436c5e3d568bd0e938f6ed58b77a48aba15",
    remote = "https://github.com/googleapis/googleapis.git",
)

new_go_repository(
    name = "com_github_google_go_genproto",
    commit = "08f135d1a31b6ba454287638a3ce23a55adace6f",
    importpath = "google.golang.org/genproto",
)

new_go_repository(
    name = "org_golang_google_grpc",
    commit = "8712952b7d646dbbbc6fb73a782174f3115060f3",
    importpath = "google.golang.org/grpc",
)

new_go_repository(
    name = "com_github_spf13_cobra",
    commit = "9495bc009a56819bdb0ddbc1a373e29c140bc674",
    importpath = "github.com/spf13/cobra",
)

new_go_repository(
    name = "com_github_spf13_pflag",
    commit = "5ccb023bc27df288a957c5e994cd44fd19619465",
    importpath = "github.com/spf13/pflag",
)

new_git_repository(
    name = "com_github_istio_api",
    build_file = "BUILD.api",
    commit = "1d9417f607be5503eee95fdb109c0d906fe6b5f5",
    remote = "https://github.com/istio/api.git",
)

# use with --define istio=local invocation
new_local_repository(
    name = "local_istio_api",
    build_file = "BUILD.api",
    path = "../api",
)
