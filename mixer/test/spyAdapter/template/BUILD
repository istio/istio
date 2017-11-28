# gazelle:exclude template.gen.go

load("@io_bazel_rules_go//go:def.bzl", "go_library")

package(default_visibility = ["//visibility:public"])

load("//mixer/tools/codegen:generate.bzl", "mixer_supported_template_library")

mixer_supported_template_library(
    name = "go_default_library",
    packages = {
        "//mixer/test/spyAdapter/template/report:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/test/spyAdapter/template/report",
    },
    deps = [
        "//mixer/test/spyAdapter/template/report:go_default_library",
    ],
)
