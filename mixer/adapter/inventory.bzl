load("@io_bazel_rules_go//go:def.bzl", "go_library")

def _inventory_gen(name, packages, out):
  args = ""
  for k, v in packages.items():
    args += "-p %s:%s " % (k, v)
  
  native.genrule(
      name = name+"_gen",
      outs = [out],
      cmd = "$(location //mixer/tools/codegen/cmd/mixgeninventory) " + args + " -o $(location %s)" % (out),
      tools = ["//mixer/tools/codegen/cmd/mixgeninventory"],
  )

DEPS = [
    "//mixer/pkg/adapter:go_default_library",
    "//mixer/adapter/kubernetes:go_default_library",
    "//mixer/adapter/noopLegacy:go_default_library",
]

def inventory_library(name, packages, deps):
  _inventory_gen("inventory_file_gen", packages, "inventory.gen.go")

  go_library(
      name = name,
      srcs = ["inventory.gen.go", "inventory.go"],
      deps = deps + DEPS,
  )
