load("@io_bazel_rules_go//go:def.bzl", "go_prefix", "go_binary")

genrule(
    name = "process_cfgeditor_content",
    srcs = glob([
        "content/**/*",
    ]),
    outs = ["content.go"],
    cmd = "go-bindata -o $(OUTS) --prefix contrib/cfgeditor/content contrib/cfgeditor/content/...",
)

go_binary(
    name = "cfgeditor",
    srcs = [
        "content.go",
        "main.go",
    ],
    deps = [
        "@com_github_spf13_cobra//:go_default_library",
    ],
)
