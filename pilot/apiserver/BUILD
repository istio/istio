load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "apiserver.go",
        "config.go",
        "handler.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["apiserver_test.go"],
    data = glob(["testdata/*.golden"]),
    library = ":go_default_library",
    deps = [
        "//model:go_default_library",
        "//test/mock:go_default_library",
        "//test/util:go_default_library",
        "@com_github_emicklei_go_restful//:go_default_library",
    ],
)
