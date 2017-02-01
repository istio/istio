workspace(name = "com_github_istio_mixer")

git_repository(
    name = "io_bazel_rules_go",
    commit = "76c63b5cd0d47c1f2b47ab4953db96c574af1c1d",
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

new_go_repository(
    name = "com_github_hashicorp_go_multierror",
    commit = "8484912a3b9987857bac52e0c5fec2b95f419628",
    importpath = "github.com/hashicorp/go-multierror",
)

new_go_repository(
    name = "com_github_hashicorp_errwrap",
    commit = "7554cd9344cec97297fa6649b055a8c98c2a1e55",
    importpath = "github.com/hashicorp/errwrap",
)

new_go_repository(
    name = "com_github_opentracing_opentracing_go",
    commit = "ac5446f53f2c0fc68dc16dc5f426eae1cd288b34",
    importpath = "github.com/opentracing/opentracing-go",
)

new_go_repository(
    name = "com_github_opentracing_basictracer",
    commit = "1b32af207119a14b1b231d451df3ed04a72efebf",
    importpath = "github.com/opentracing/basictracer-go",
)

# Transitive dep of com_github_opentracing_basictracer
new_go_repository(
    name = "com_github_gogo_protobuf",
    commit = "909568be09de550ed094403c2bf8a261b5bb730a",
    importpath = "github.com/gogo/protobuf",
)

new_go_repository(
    name = "com_github_mitchellh_mapstructure",
    commit = "bfdb1a85537d60bc7e954e600c250219ea497417",
    importpath = "github.com/mitchellh/mapstructure",
)

load("//:repositories.bzl", "new_git_or_local_repository")

new_git_or_local_repository(
    name = "com_github_istio_api",
    build_file = "BUILD.api",
    path = "../api",
    commit = "7917b2d041a9ef931e242ae58caa584158eb4cf3",
    remote = "https://github.com/istio/api.git",
    # Change this to True to use ../api directory
    use_local = False,
)

new_http_archive(
    name = "docker_ubuntu",
    build_file = "BUILD.ubuntu",
    sha256 = "2c63dd81d714b825acd1cb3629c57d6ee733645479d0fcdf645203c2c35924c5",
    type = "zip",
    url = "https://codeload.github.com/tianon/docker-brew-ubuntu-core/zip/b6f1fe19228e5b6b7aed98dcba02f18088282f90",
)

new_go_repository(
    name = "com_github_prometheus_client_golang",
    commit = "c317fb74746eac4fc65fe3909195f4cf67c5562a",
    importpath = "github.com/prometheus/client_golang",
)

new_go_repository(
    name = "com_github_prometheus_common",
    commit = "dd2f054febf4a6c00f2343686efb775948a8bff4",
    importpath = "github.com/prometheus/common",
)

new_go_repository(
    name = "com_github_matttproud_golang_protobuf_extensions",
    commit = "c12348ce28de40eed0136aa2b644d0ee0650e56c",
    importpath = "github.com/matttproud/golang_protobuf_extensions",
)

new_go_repository(
    name = "com_github_prometheus_procfs",
    commit = "fcdb11ccb4389efb1b210b7ffb623ab71c5fdd60",
    importpath = "github.com/prometheus/procfs",
)

new_go_repository(
    name = "com_github_beorn7_perks",
    commit = "4c0e84591b9aa9e6dcfdf3e020114cd81f89d5f9",
    importpath = "github.com/beorn7/perks",
)

new_go_repository(
    name = "com_github_prometheus_client_model",
    commit = "fa8ad6fec33561be4280a8f0514318c79d7f6cb6",
    importpath = "github.com/prometheus/client_model",
)
