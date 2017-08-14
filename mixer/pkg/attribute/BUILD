package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "bag.go",
        "dictState.go",
        "emptyBag.go",
        "list.gen.go",
        "mutableBag.go",
        "protoBag.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/pool:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_istio_api//:mixer/v1",  # keep
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["bag_test.go"],
    library = ":go_default_library",
    deps = [
        "@com_github_istio_api//:mixer/v1",  # keep
    ],
)

genrule(
    name = "global_list",
    srcs = ["@com_github_istio_api//:mixer/v1/attributes_file"],
    outs = ["list.gen.go"],
    cmd = "$(location //:generate_word_list) $(location @com_github_istio_api//:mixer/v1/attributes_file) | $(location @org_golang_x_tools_imports//:goimports) > $@",
    message = "Generating word list",
    tools = [
        "//:generate_word_list",
        "@org_golang_x_tools_imports//:goimports",
    ],
    visibility = ["//visibility:private"],
)
