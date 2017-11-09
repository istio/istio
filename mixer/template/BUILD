package(default_visibility = ["//visibility:public"])

load("//mixer/tools/codegen:generate.bzl", "mixer_supported_template_library")

mixer_supported_template_library(
    name = "go_default_library",
    packages = {
        "//mixer/adapter/svcctrl/template/svcctrlreport:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/adapter/svcctrl/template/svcctrlreport",
        "//mixer/template/apikey:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/template/apikey",
        "//mixer/template/checknothing:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/template/checknothing",
        "//mixer/template/listentry:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/template/listentry",
        "//mixer/template/logentry:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/template/logentry",
        "//mixer/template/metric:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/template/metric",
        "//mixer/template/quota:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/template/quota",
        "//mixer/template/reportnothing:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/template/reportnothing",
        "//mixer/template/tracespan:go_default_library_proto.descriptor_set": "istio.io/istio/mixer/template/tracespan",
    },
    deps = [
        "//mixer/adapter/svcctrl/template/svcctrlreport:go_default_library",
        "//mixer/template/apikey:go_default_library",
        "//mixer/template/checknothing:go_default_library",
        "//mixer/template/listentry:go_default_library",
        "//mixer/template/logentry:go_default_library",
        "//mixer/template/metric:go_default_library",
        "//mixer/template/quota:go_default_library",
        "//mixer/template/reportnothing:go_default_library",
        "//mixer/template/tracespan:go_default_library",
    ],
)
