# Helm Profiles

This folder provides a variety of "profiles" for helm installation.
While a user can just explicitly pass this with `--values/-f`.

However, Istio also provides a feature to bundle these with the charts, which is convenient when installing from remote charts.
For details, see `copy-templates` Makefile target, and `manifests/zzz_profile.yaml`.

Any changes to this folder should have a `make copy-templates` applied afterwards.

Warning: unlike the `IstioOperator` profiles, these profiles cannot enable or disable certain components.
As a result, users still need to ensure they install the appropriate charts to use a profile correctly.
These requirements are documented in each profile.
