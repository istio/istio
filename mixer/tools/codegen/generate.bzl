load("@org_pubref_rules_protobuf//protobuf:rules.bzl", "proto_compile")
load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library", "gogo_proto_library")
load("@io_bazel_rules_go//go:def.bzl", "go_library")

MIXER_DEPS = [
    "@com_github_istio_mixer//pkg/adapter:go_default_library",
    "@com_github_istio_api//:mixer/v1/template",
    "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
]

MIXER_INPUTS = [
    "@com_github_istio_api//:mixer/v1/template_protos",
    "@com_github_istio_api//:mixer/v1/config/descriptor_protos",  # keep
]

MIXER_IMPORT_MAP = {
    "mixer/v1/config/descriptor/value_type.proto": "istio.io/api/mixer/v1/config/descriptor",
    "mixer/v1/template/extensions.proto": "istio.io/api/mixer/v1/template",
}

# TODO: develop better approach to import management.
# including the "../.." is an ugly workaround for differing exec ctx for bazel rules
# depending on whether or not we are building within mixer proper or in a third-party repo
# that depends on mixer proper.
MIXER_IMPORTS = [
    "external/com_github_istio_api",
    "../../external/com_github_istio_api",
]

# TODO: fill in with complete set of GOGO DEPS and IMPORT MAPPING
GOGO_DEPS = [
    "@com_github_gogo_protobuf//gogoproto:go_default_library",
    "@com_github_gogo_protobuf//types:go_default_library",
    "@com_github_gogo_protobuf//sortkeys:go_default_library",
]

GOGO_IMPORT_MAP = {
    "gogoproto/gogo.proto": "github.com/gogo/protobuf/gogoproto",
    "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
}

# TODO: develop better approach to import management.
# including the "../.." is an ugly workaround for differing exec ctx for bazel rules
# depending on whether or not we are building within mixer proper or in a third-party repo
# that depends on mixer proper.
PROTO_IMPORTS = [
    "external/com_github_google_protobuf/src",
    "../../external/com_github_google_protobuf/src",
]

PROTO_INPUTS = ["@com_github_google_protobuf//:well_known_protos"]

def _gen_template_and_handler(name, importmap = {}):
   m = ""
   for k, v in importmap.items():
      m += " -m %s:%s" % (k, v)

   src_desc = name + "_proto.descriptor_set"
   gen_handler = name + "_handler.gen.go"
   gen_tmpl = name + "_tmpl.proto"

   genrule_args = {
       "name": name + "_handler",
       "srcs": [ src_desc ],
       "outs": [ gen_handler, gen_tmpl ],
       "tools": [ "@com_github_istio_mixer//tools/codegen/cmd/mixgenproc" ],
       "message": "Generating handler code from descriptor",
       "cmd": "$(location @com_github_istio_mixer//tools/codegen/cmd/mixgenproc) "
            + "$(location %s) -o=$(location %s) -t=$(location %s) %s" % (src_desc, gen_handler, gen_tmpl, m)
   }

   native.genrule(**genrule_args)

def mixer_proto_library(
    name,
    protos = [],
    importmap = {},
    imports = [],
    inputs = [],
    deps = [],
    verbose = 0,
    proto_compile_args = {},
    mixer_gen_args = {},
    gogoslick_args = {},
    **kwargs):

   proto_compile_args += {
     "name": name + "_proto",
     "args" : ["--include_imports", "--include_source_info"],
     "protos": protos,
     "importmap": importmap,
     "imports": imports + MIXER_IMPORTS + PROTO_IMPORTS,
     "inputs": inputs + MIXER_INPUTS + PROTO_INPUTS,
     "verbose": verbose,
   }

   # we must run proto compile, as the mixer gen depends on the args
   # for including imports and there isn't a way to pass those args
   # through the gogo_proto_* methods at the moment.
   proto_compile(**proto_compile_args)

   importmap = dict(dict(MIXER_IMPORT_MAP, **GOGO_IMPORT_MAP), **importmap)
   _gen_template_and_handler(name, importmap)

   gogoslick_args += {
      "name": name + "_gogo_proto",
      "protos": [name + "_tmpl.proto"],
      "deps": MIXER_DEPS + GOGO_DEPS + deps,
      "imports": imports + MIXER_IMPORTS + PROTO_IMPORTS,
      "importmap": dict(dict(MIXER_IMPORT_MAP, **GOGO_IMPORT_MAP), **importmap),
      "inputs": inputs + MIXER_INPUTS + PROTO_INPUTS,
      "verbose": verbose,
   }

   # we run this proto library to get the generated pb.go files to link
   # in with the mixer generated files for a go library
   gogoslick_proto_library(**gogoslick_args)

   go_library(
      name = name,
      srcs = [name + "_handler.gen.go"],
      library = ":" + name + "_gogo_proto",
      deps = deps + MIXER_DEPS)

###############

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
      cmd = "$(location @com_github_istio_mixer//tools/codegen/cmd/mixgenbootstrap) " + args + " -o $(location %s)" % (out),
      tools = ["@com_github_istio_mixer//tools/codegen/cmd/mixgenbootstrap"],
  )

DEPS_FOR_ALL_TMPLS = [
    "@com_github_istio_mixer//pkg/adapter:go_default_library",
    "@com_github_istio_api//:mixer/v1/template",
    "@com_github_istio_mixer//pkg/attribute:go_default_library",
    "@com_github_istio_mixer//pkg/expr:go_default_library",
    "@com_github_istio_mixer//pkg/template:go_default_library",
    "@com_github_gogo_protobuf//proto:go_default_library",
    "@com_github_golang_glog//:go_default_library",
    "@com_github_istio_api//:mixer/v1/config/descriptor",  # keep
]

def mixer_supported_template_library(name, packages, deps):
  _mixer_supported_template_gen("mixer_supported_template_file_gen", packages, "template.gen.go")

  go_library(
      name = name,
      srcs = ["template.gen.go"],
      deps = deps + DEPS_FOR_ALL_TMPLS,
  )
