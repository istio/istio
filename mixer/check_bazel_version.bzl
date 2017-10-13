def _parse_bazel_version(bazel_version):
    # Remove commit from version.
    version = bazel_version.split(" ", 1)[0]

    # Split into (release, date) parts and only return the release
    # as a tuple of integers.
    parts = version.split("-", 1)

    # Turn "release" into a tuple of strings
    version_tuple = ()
    for number in parts[0].split("."):
        version_tuple += (int(number),)
    return version_tuple

# acceptable min_version <= version <= max_version
def check_version():
    check_bazel_version("0.5.3", "0.5.4")

# acceptable min_version <= version <= max_version
def check_bazel_version(min_version, max_version):
    if "bazel_version" not in dir(native):
        fail("\nCurrent Bazel version is lower than 0.2.1, expected at least %s\n" %
             min_version)
    elif not native.bazel_version:
        print("\nCurrent Bazel is not a release version, cannot check for " +
              "compatibility.")
        print("Make sure that you are running at least Bazel %s.\n" % min_version)
    else:
        _version = _parse_bazel_version(native.bazel_version)
        _min_version = _parse_bazel_version(min_version)
        _max_version = _parse_bazel_version(max_version)

        if _version < _min_version:
            fail("\nCurrent Bazel version {} is too old, expected at least {}\n".format(
                native.bazel_version, min_version))

        if _version > _max_version:
            fail("\nCurrent Bazel version {} is too new, expected at most {}\n".format(
                native.bazel_version, max_version))
