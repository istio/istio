# README

This folder contains config files for creating [Tailorbird](go/tailorbird)
resources. Instructions to write the yaml spec can be found in its [user
guide](go/tbird-user-guide).

Note the file names must be in the format of [cluster-type]-[topology],
which correspond to the `--cluster-type` and `--topology` flag value passed via
`integ-suite-kubetest2.sh` when configuring Prow jobs.
