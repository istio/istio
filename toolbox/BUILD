load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
    	"main.go",
    	"dependency.go",
    	"githubClient.go"
    ],
    deps = [
    	"//tests/e2e/util:go_default_library",
        "@com_github_google_go_github//github:go_default_library",
        "@org_golang_x_oauth2//:go_default_library",
    ],
)

go_binary(
    name = "update_deps",
    library = ":go_default_library",
)
