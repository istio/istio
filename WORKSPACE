workspace(name = "io_istio_api")

load("//:check_bazel_version.bzl", "check_version")
check_version()

git_repository(
    name = "io_bazel_rules_go",
    commit = "9cf23e2aab101f86e4f51d8c5e0f14c012c2161c",  # Oct 12, 2017 (Add `build_external` option to `go_repository`)
    remote = "https://github.com/bazelbuild/rules_go.git",
)

load("@io_bazel_rules_go//go:def.bzl", "go_rules_dependencies", "go_register_toolchains", "go_repository")
go_rules_dependencies()
go_register_toolchains()

load("@io_bazel_rules_go//proto:def.bzl", "proto_register_toolchains")
proto_register_toolchains()

load("//:x_tools_imports.bzl", "go_x_tools_imports_repositories")
go_x_tools_imports_repositories()

load("//:googleapis.bzl", "go_googleapis_repositories")
go_googleapis_repositories()

git_repository(
    name = "org_pubref_rules_protobuf",
    commit = "ff3b7e7963daa7cb3b42f8936bc11eda4b960926",  # Oct 03, 2017 (Updating External Import Paths)
    remote = "https://github.com/pubref/rules_protobuf",
)

go_repository(
    name = "com_github_golang_protobuf",
    commit = "17ce1425424ab154092bbb43af630bd647f3bb0d",  # Nov 16, 2016 (match pubref dep)
    importpath = "github.com/golang/protobuf",
)

go_repository(
    name = "com_github_gogo_protobuf",
    commit = "100ba4e885062801d56799d78530b73b178a78f3",  # Mar 7, 2017 (match pubref dep)
    importpath = "github.com/gogo/protobuf",
    build_file_proto_mode = "legacy",
)

bind(
    name = "protoc",
    actual = "@com_google_protobuf//:protoc",
)

bind(
    name = "protocol_compiler",
    actual = "@com_google_protobuf//:protoc",
)
