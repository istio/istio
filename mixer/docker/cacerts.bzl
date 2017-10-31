"""A rule to unpack ca certificates from the debian package."""

""" Taken, verbatim, from: https://github.com/GoogleCloudPlatform/distroless/blob/master/cacerts/extract.sh
It was copied, as the distroless skylark rules/build for ca-certs have restricted visibility."""

def _impl(ctx):
    args = "%s %s %s" % (ctx.executable._extract.path, ctx.file.deb.path, ctx.outputs.out.path)
    ctx.action(command = args,
            inputs = [ctx.executable._extract, ctx.file.deb],
            outputs = [ctx.outputs.out])

cacerts = rule(
    attrs = {
        "deb": attr.label(
            default = Label("@ca-certificates//file:pkg.deb"),
            allow_files = [".deb"],
            single_file = True,
        ),
        # Implicit dependencies.
        "_extract": attr.label(
            default = Label("//mixer/docker:extract_certs"),
            cfg = "host",
            executable = True,
            allow_files = True,
        ),
    },
    executable = False,
    outputs = {
        "out": "%{name}.tar",
    },
    implementation = _impl,
)
