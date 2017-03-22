load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["inject.go"],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_ghodss_yaml//:go_default_library",
        "@io_k8s_client_go//pkg/api/v1:go_default_library",
        "@io_k8s_client_go//pkg/apis/extensions/v1beta1:go_default_library",
        "@io_k8s_client_go//pkg/apis/meta/v1:go_default_library",
        "@io_k8s_client_go//pkg/util/yaml:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["inject_test.go"],
    data = glob(["testdata/*.yaml*"]),
    library = ":go_default_library",
    deps = ["@com_github_pmezard_go_difflib//difflib:go_default_library"],
)
