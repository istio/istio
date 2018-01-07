package(default_visibility = [
    "//mixer/adapter/solarwinds:__pkg__",
])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
		"batching.go",
        "client.go",
        "measurements.go",
        "mock_client.go",
        "mock_measurements_service.go",
    ],
    importpath = "istio.io/istio/mixer/adapter/solarwinds/appoptics",
    visibility = [
        "//mixer/adapter/solarwinds:__pkg__",
    ],
    deps = [
        "//mixer/pkg/adapter:go_default_library",
		"//mixer/adapter/solarwinds/config:go_default_library",
    ]
)

go_test(
    name = "go_default_test",
    srcs = ["batching_test.go", "client_test.go", "measurements_test.go"],
    importpath = "istio.io/istio/mixer/adapter/solarwinds/appoptics",
    library = ":go_default_library",
	deps = [
		"//mixer/pkg/adapter:go_default_library",
		"//mixer/adapter/solarwinds/papertrail:go_default_library",
	]
)
