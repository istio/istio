load("@io_bazel_rules_go//go:def.bzl", "go_repository")

def mixer_adapter_repositories():

    native.git_repository(
        name = "org_pubref_rules_protobuf",
        commit = "563b674a2ce6650d459732932ea2bc98c9c9a9bf",  # Nov 28, 2017 (bazel 0.8.0 support)
        remote = "https://github.com/pubref/rules_protobuf",
    )

    native.bind(
        name = "protoc",
        actual = "@com_google_protobuf//:protoc",
    )

    native.bind(
        name = "protocol_compiler",
        actual = "@com_google_protobuf//:protoc",
    )

    go_repository(
        name = "org_golang_x_net",
        commit = "f5079bd7f6f74e23c4d65efa0f4ce14cbd6a3c0f",  # Jul 26, 2017 (no releases)
        importpath = "golang.org/x/net",
    )

    go_repository(
        name = "com_github_golang_protobuf",
        commit = "17ce1425424ab154092bbb43af630bd647f3bb0d",  # Nov 16, 2016 (match pubref dep)
        importpath = "github.com/golang/protobuf",
    )

    go_repository(
        name = "com_github_gogo_protobuf",
        commit = "342cbe0a04158f6dcb03ca0079991a51a4248c02",  # Oct 7, 2017 (v0.5)
        importpath = "github.com/gogo/protobuf",
        build_file_proto_mode = "legacy",
    )

    go_repository(
        name = "org_golang_google_grpc",
        commit = "f92cdcd7dcdc69e81b2d7b338479a19a8723cfa3",  # Aug 30, 2017 (v1.6.0)
        importpath = "google.golang.org/grpc",
    )

    go_repository(
        name = "org_golang_x_text",
        build_file_name = "BUILD.bazel",
        commit = "f4b4367115ec2de254587813edaa901bc1c723a8",  # Mar 31, 2017 (no releases)
        importpath = "golang.org/x/text",
    )

    go_repository(
        name = "io_istio_gogo_genproto",
        commit = "09740ece0bc45a1cd0971a8b1f57c44b13ccd8dd",  # Dec 14, 2017 (initial generation of status protos)
        importpath = "istio.io/gogo-genproto",
    )
