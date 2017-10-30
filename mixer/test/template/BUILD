package(default_visibility = ["//visibility:public"])

load("//tools/codegen:generate.bzl", "mixer_supported_template_library")

mixer_supported_template_library(
    name = "go_default_library",
    packages = {
        "//test/template/report:go_default_library_proto.descriptor_set": "istio.io/mixer/test/template/report",
    },
    deps = [
        "//test/template/report:go_default_library",
    ],
)
