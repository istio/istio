load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["common.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/stackdriver/config:go_default_library",
        "@org_golang_google_api//option:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["common_test.go"],
    library = ":go_default_library",
    deps = [
        "//adapter/stackdriver/config:go_default_library",
        "@org_golang_google_api//option:go_default_library",
    ],
)
