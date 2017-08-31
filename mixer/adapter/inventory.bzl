load("@io_bazel_rules_go//go:def.bzl", "go_library")

def _inventory_gen(name, packages, out):
  args = ""
  for k, v in packages.items():
    args += "-p %s:%s " % (k, v)
  
  native.genrule(
      name = name+"_gen",
      outs = [out],
      cmd = "$(location //tools/codegen/cmd/mixgeninventory) " + args + " -o $(location %s)" % (out),
      tools = ["//tools/codegen/cmd/mixgeninventory"],
  )

DEPS = [
    "//pkg/adapter:go_default_library",
    "//pkg/handler:go_default_library",
]

def inventory_library(name, packages, deps):
  _inventory_gen("inventory_file_gen", packages, "inventory.gen.go")

  go_library(
      name = name,
      srcs = ["inventory.gen.go"],      
      deps = deps + DEPS,
  )
 
