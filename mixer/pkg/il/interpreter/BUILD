load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "extern.go",
        "interpreter.go",
        "interpreterRun.go",
        "result.go",
        "stackFrame.go",
        "stepper.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/attribute:go_default_library",
        "//pkg/il:go_default_library",
        "//pkg/il/text:go_default_library",
        "@com_github_golang_glog//:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = [
        "extern_test.go",
        "interpreterBenchmark_test.go",
        "interpreter_test.go",
        "result_test.go",
    ],
    library = ":go_default_library",
    deps = [
        "//pkg/config/proto:go_default_library",
        "//pkg/expr:go_default_library",
        "//pkg/il:go_default_library",
        "//pkg/il/testing:go_default_library",
        "//pkg/il/text:go_default_library",
        "@io_istio_api//:mixer/v1/config/descriptor",
    ],
)
