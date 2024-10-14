go_library(
    name = "builder",
    srcs = [
        "builder.go",
        "extauthz.go",
        "logger.go",
    ],
)

go_test(
    name = "builder_test",
    srcs = [
        "builder_test.go",
        "fuzz_test.go",
    ],
    library = ":builder",
)
