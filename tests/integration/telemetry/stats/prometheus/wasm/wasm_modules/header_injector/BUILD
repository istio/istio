load("@proxy_wasm_cpp_sdk//bazel/wasm:wasm.bzl", "wasm_cc_binary")

wasm_cc_binary(
    name = "plugin-0.0.1.wasm",
    srcs = [
        "plugin.cc",
        "plugin.h",
    ],
    deps = [
        "@proxy_wasm_cpp_sdk//:proxy_wasm_intrinsics",
    ],
    defines = [
        "INJECTION_VERSION=0.0.1"
    ],
)

wasm_cc_binary(
    name = "plugin-0.0.2.wasm",
    srcs = [
        "plugin.cc",
        "plugin.h",
    ],
    deps = [
        "@proxy_wasm_cpp_sdk//:proxy_wasm_intrinsics",
    ],
    defines = [
        "INJECTION_VERSION=0.0.2"
    ],
)