package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "bag.go",
        "dictState.go",
        "emptyBag.go",
        "list.gen.go",  # keep
        "mutableBag.go",
        "protoBag.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//mixer/pkg/pool:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@io_istio_api//:mixer/v1",
    ],
)

go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["bag_test.go"],
    library = ":go_default_library",
    deps = ["@io_istio_api//:mixer/v1"],
)

genrule(
    name = "global_list",
    srcs = ["@io_istio_api//:mixer/v1/attributes_file"],
    outs = ["list.gen.go"],
    cmd = "$(location //mixer:generate_word_list) $(location @io_istio_api//:mixer/v1/attributes_file) | $(location @org_golang_x_tools_imports//:goimports) > $@",
    message = "Generating word list",
    tools = [
        "//mixer:generate_word_list",
        "@org_golang_x_tools_imports//:goimports",
    ],
    visibility = ["//visibility:private"],
)
