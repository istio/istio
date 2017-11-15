# gazelle:exclude go_default_library_handler.gen.go
# gazelle:exclude go_default_library_tmpl.pb.go

load("@io_bazel_rules_go//go:def.bzl", "go_library")

package(default_visibility = ["//visibility:public"])

load("//mixer/tools/codegen:generate.bzl", "mixer_proto_library")

mixer_proto_library(
    name = "go_default_library",
    protos = ["QuotaTesterTemplate.proto"],
)
