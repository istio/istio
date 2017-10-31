package(default_visibility = ["//visibility:public"])

load("//mixer/tools/codegen:generate.bzl", "mixer_supported_template_library")

mixer_supported_template_library(
    name = "go_default_library",
    packages = {
        "//mixer/test/template/report:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/test/template/report",
    },
    deps = [
        "//mixer/test/template/report:go_default_library",
    ],
)
