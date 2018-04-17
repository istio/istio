# Troubleshooting

- If you experience the following error during build:

_grouped write of manifest, lock and vendor: error while writing out vendor tree: failed to write dep tree:
 failed to export google.golang.org/api: cannot stat /home/user/go/pkg/dep/sources/https---code.googlesource.com-google--api--go--client/.git/index: stat /home/user/go/pkg/dep/sources/https---code.googlesource.com-google--api--go--client/.git/index: no such file or directory_

 Just remove the package, in this case `/home/user/go/pkg/dep/sources/https---code.googlesource.com-google--api--go--client` and rerun build.
