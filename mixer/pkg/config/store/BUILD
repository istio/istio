package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "convert.go",
        "fsstore.go",
        "fsstore2.go",
        "memstore.go",
        "queue.go",
        "store.go",
        "store2.go",
        "testutil.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//jsonpb:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "convert_test.go",
        "fsstore2_test.go",
        "fsstore_test.go",
        "queue_test.go",
        "store2_test.go",
        "store_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//mixer/pkg/config/proto:go_default_library",
        "@com_github_ghodss_yaml//:go_default_library",
        "@com_github_gogo_protobuf//proto:go_default_library",
    ],
)
