load("@io_bazel_rules_go//go:def.bzl", "go_library")

def _mixer_supported_template_gen(name, packages, out):
  args = ""
  descriptors = []
  for k1, v in packages.items():
    l = "$(location %s)" % (k1)
    args += " %s:%s " % (l, v)
    descriptors.append(k1)

  native.genrule(
      name = name+"_gen",
      srcs = descriptors,
      outs = [out],
      cmd = "$(location @io_istio_istio//mixer/tools/codegen/cmd/mixgenbootstrap) " + args + " -o $(location %s)" % (out),
      tools = ["@io_istio_istio//mixer/tools/codegen/cmd/mixgenbootstrap"],
  )

DEPS_FOR_ALL_TMPLS = [
    "@io_istio_istio//mixer/pkg/adapter:go_default_library",
    "@io_istio_istio//mixer/pkg/attribute:go_default_library",
    "@io_istio_istio//mixer/pkg/expr:go_default_library",
    "@io_istio_istio//mixer/pkg/config/proto:go_default_library",
    "@io_istio_istio//mixer/pkg/template:go_default_library",
    "@io_istio_istio//pkg/log:go_default_library",
    "@com_github_gogo_protobuf//proto:go_default_library",
    "@io_istio_api//mixer/v1/config/descriptor:go_default_library",
    "@io_istio_api//mixer/v1/template:go_default_library",
]

def mixer_supported_template_library(name, packages, deps):
  _mixer_supported_template_gen("mixer_supported_template_file_gen", packages, "template.gen.go")

  go_library(
      name = name,
      srcs = ["template.gen.go"],
      deps = deps + DEPS_FOR_ALL_TMPLS,
  )
