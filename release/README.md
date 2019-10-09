# Istio Release Process

- [Istio Release Process](#istio-release-process)
    - [Daily Releases](#daily-releases)

## Daily Releases

Every day, a new release is automatically built and made available at
<https://gcsweb.istio.io/gcs/istio-prerelease/daily-build/>
after it passes the automatic daily release tests. These are meant for developer testing, but not general consumption.

Each daily release is versioned and identified by `<branch_name>-<build_datetime>`. For example,
e.g. `master-20180615-09-15` is cut from master at 9:15AM GMT on June 15, 2018. Daily release artifacts are stored in
sub-directories using the same naming scheme in [GCS](https://gcsweb.istio.io/gcs/istio-prerelease/daily-build/).

