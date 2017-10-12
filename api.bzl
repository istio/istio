load("@io_bazel_rules_go//go:def.bzl", "go_repository")

def protobuf_repositories(bind=True):
    PROTOBUF_SHA = "c4083bb3d1231f8a94f2f000434e38528bdff64a" # Oct 10, 2017

    native.http_archive(
        name = "com_google_protobuf",
        strip_prefix = "protobuf-" + PROTOBUF_SHA,
        urls = ["https://github.com/google/protobuf/archive/" + PROTOBUF_SHA + ".tar.gz"],
    )

    native.http_archive(
        name = "com_google_protobuf_cc",
        strip_prefix = "protobuf-" + PROTOBUF_SHA,
        urls = ["https://github.com/google/protobuf/archive/" + PROTOBUF_SHA + ".tar.gz"],
    )

    native.http_archive(
        name = "com_github_google_protobuf",
        strip_prefix = "protobuf-" + PROTOBUF_SHA,
        urls = ["https://github.com/google/protobuf/archive/" + PROTOBUF_SHA + ".tar.gz"],
    )

    if bind:
        native.bind(
            name = "protoc",
            actual = "@com_google_protobuf//:protoc",
        )

        native.bind(
            name = "protocol_compiler",
            actual = "@com_google_protobuf//:protoc",
        )

def go_istio_api_dependencies(bind=True):
    protobuf_repositories(bind)

    native.git_repository(
        name = "org_pubref_rules_protobuf",
        commit = "eafd42ce6471ce3ea265729c85e18e6180dea620",  # Sept 22, 2017 (genfiles path calculation fix)
        remote = "https://github.com/pubref/rules_protobuf",
    )

    go_repository(
        name = "com_github_golang_glog",
        commit = "23def4e6c14b4da8ac2ed8007337bc5eb5007998",  # Jan 26, 2016 (no releases)
        importpath = "github.com/golang/glog",
    )

    go_repository(
        name = "org_golang_x_net",
        commit = "f5079bd7f6f74e23c4d65efa0f4ce14cbd6a3c0f",  # Jul 26, 2017 (no releases)
        importpath = "golang.org/x/net",
    )

    go_repository(
        name = "org_golang_x_text",
        build_file_name = "BUILD.bazel",
        commit = "f4b4367115ec2de254587813edaa901bc1c723a8",  # Mar 31, 2017 (no releases)
        importpath = "golang.org/x/text",
    )

    go_repository(
        name = "org_golang_google_genproto",
        commit = "aa2eb687b4d3e17154372564ad8d6bf11c3cf21f",  # June 1, 2017 (no releases)
        importpath = "google.golang.org/genproto",
    )

    go_repository(
        name = "com_github_golang_protobuf",
        commit = "8ee79997227bf9b34611aee7946ae64735e6fd93",  # Nov 16, 2016 (match pubref dep)
        importpath = "github.com/golang/protobuf",
    )

    go_repository(
        name = "com_github_gogo_protobuf",
        commit = "100ba4e885062801d56799d78530b73b178a78f3",  # Mar 7, 2017 (match pubref dep)
        importpath = "github.com/gogo/protobuf",
    )

    go_repository(
        name = "org_golang_google_grpc",
        commit = "d2e1b51f33ff8c5e4a15560ff049d200e83726c5",  # April 28, 2017 (v1.3.0)
        importpath = "google.golang.org/grpc",
    )

    X_IMPORTS_BUILD_FILE = """
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_binary")
load("@io_bazel_rules_go//go:def.bzl", "go_prefix")

go_prefix("golang.org/x/tools")

licenses(["notice"])  # New BSD

exports_files(["LICENSE"])

go_binary(
    name = "goimports",
    srcs = [
        "cmd/goimports/doc.go",
        "cmd/goimports/goimports.go",
        "cmd/goimports/goimports_gc.go",
        "cmd/goimports/goimports_not_gc.go",
    ],
    deps = [
        "@org_golang_x_tools//imports:go_default_library",
    ],
)
"""
    # bazel rule for fixing up cfg.pb.go relies on running goimports
    # we import it here as a git repository to allow projection of a
    # simple build rule that will build the binary for usage (and avoid
    # the need to project a more complicated BUILD file over the entire
    # tools repo.)
    native.new_git_repository(
        name = "org_golang_x_tools_imports",
        build_file_content = X_IMPORTS_BUILD_FILE,
        commit = "e6cb469339aef5b7be0c89de730d5f3cc8e47e50",  # Jun 23, 2017 (no releases)
        remote = "https://github.com/golang/tools.git",
    )

    GOOGLEAPIS_BUILD_FILE = """
package(default_visibility = ["//visibility:public"])

filegroup(
    name = "protos",
    srcs = [
        "google/rpc/code.proto",
        "google/rpc/error_details.proto",
        "google/rpc/status.proto",
    ],
)

load("@org_pubref_rules_protobuf//cpp:rules.bzl", "cc_proto_library")

cc_proto_library(
    name = "cc_status_proto",
    protos = [
        "google/rpc/status.proto",
    ],
    imports = [
        "../../external/com_google_protobuf/src",
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

filegroup(
    name = "error_details_proto",
    srcs = [ "google/rpc/error_details.proto" ],
)

filegroup(
    name = "label_proto",
    srcs = [
        "google/api/label.proto",
    ],
)

filegroup(
    name = "metric_proto",
    srcs = [
        "google/api/metric.proto",
    ],
)
"""

    GOOGLEAPIS_SHA = "13ac2436c5e3d568bd0e938f6ed58b77a48aba15" # Oct 21, 2016 (only release pre-dates sha)

    native.new_http_archive(
        name = "com_github_googleapis_googleapis",
        build_file_content = GOOGLEAPIS_BUILD_FILE,
        strip_prefix = "googleapis-" + GOOGLEAPIS_SHA,
        urls = ["https://github.com/googleapis/googleapis/archive/" + GOOGLEAPIS_SHA + ".tar.gz"],
        workspace_file_content = "",
    )
