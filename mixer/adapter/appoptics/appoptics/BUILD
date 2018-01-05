package(default_visibility = [
    "//mixer/adapter/appoptics:__pkg__",
    "//mixer/adapter/appoptics/promadapter:__pkg__",
])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "client.go",
        "measurements.go",
        "mock_client.go",
        "mock_measurements_service.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/appoptics/appoptics",
    visibility = [
        "//mixer/adapter/appoptics:__pkg__",
        "//mixer/adapter/appoptics/promadapter:__pkg__",
    ],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
		"//mixer/adapter/appoptics/config:go_default_library",
    ]
)

go_test(
    name = "go_default_test",
    srcs = ["client_test.go", "measurements_test.go"],
    importpath = "istio.io/istio/mixer/adapter/appoptics/appoptics",
    library = ":go_default_library",
	deps = [
		"//mixer/pkg/adapter:go_default_library",
		"//mixer/adapter/appoptics/papertrail:go_default_library",
	]
)
