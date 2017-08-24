# use_local flag is used to control
def new_git_or_local_repository(
    name,
    build_file,
    path,
    commit,
    remote,
    use_local = False):
    if use_local:
        native.new_local_repository(
            name = name,
            build_file = build_file,
            path = path
        )
    else:
        native.new_git_repository(
            name = name,
            build_file = build_file,
	    commit = commit,
	    remote = remote
        )
