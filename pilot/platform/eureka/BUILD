load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "client.go",
        "controller.go",
        "conversion.go",
        "servicediscovery.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//model:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "client_test.go",
        "controller_test.go",
        "conversion_test.go",
        "servicediscovery_test.go",
    ],
    data = glob(["testdata/*"]),
    library = ":go_default_library",
    deps = [
        "//model:go_default_library",
        "//test/util:go_default_library",
    ],
)
