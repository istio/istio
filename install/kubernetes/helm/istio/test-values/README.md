# Test Values

These files are intended to be used to install Istio for E2E tests.

The rendered files can be generated with `make generate_e2e_yaml`.

These files will all have `values-e2e.yaml` applied to them *first*, so if there are settings there that should not be included in the test the must be overridden.
