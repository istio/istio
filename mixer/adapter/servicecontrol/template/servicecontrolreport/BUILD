# gazelle:exclude go_default_library_tmpl.pb.go
# gazelle:exclude go_default_library_handler.gen.go

package(default_visibility = ["//visibility:public"])

load("//mixer/tools/codegen:generate.bzl", "mixer_proto_library")

mixer_proto_library(
    name = "go_default_library",
    protos = ["template.proto"],
)
