load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "commonUtils.go",
        "compareUtils.go",
        "processUtils.go",
        "kubeUtils.go",
        "retry.go",
        "wrk.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_golang_glog//:go_default_library",
        "@com_github_pmezard_go_difflib//difflib:go_default_library",
    ],
)
