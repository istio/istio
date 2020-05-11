# Upating charts and values.yaml

The charts in the `manifests` directory are used in istioctl to generate an installation manifest. The configuration
settings contained in values.yaml files and passed through the CLI are validated against a
[schema](../../operator/pkg/apis/istio/v1alpha1/values_types.proto).
Whenever making changes in the charts, it's important to follow the below steps.

## Step 0. Check that any schema change really belongs in values.yaml

Is this a new parameter being added? If not, go to the next step.
Dynamic, runtime config that is used to configure Istio components should go into the
[MeshConfig API](https://github.com/istio/api/blob/master/mesh/v1alpha1/config.proto). Values.yaml is being deprecated and adding
to it is discouraged. MeshConfig is the official API which follows API management practices and is dynamic
(does not require component restarts).
Exceptions to this rule are configuration items that affect K8s level settings (resources, mounts etc.)

## Step 1. Make changes in charts and values.yaml in `manifests` directory

## Step 2. Make corresponding values changes in [manifests/profiles/default.yaml](../profiles/default.yaml)

The values.yaml in `manifests` are used for direct Helm based installations.
If any values.yaml changes are being made, the same changes must be made in the `manifests/profiles/default.yaml`
file, which must be in sync with the Helm values in `manifests`.

## Step 3. Update the validation schema

Istioctl uses a [schema](../../operator/pkg/apis/istio/v1alpha1/values_types.proto) to validate the values. Any changes to
the schema must be added here, otherwise istioctl users will see errors.
Once the schema file is updated, run:

```bash
$ make operator-proto
```

This will regenerate the Go structs used for schema validation.

## Step 4. Regenerate golden files (if required)

Some schema changes (changes or deletes) may break the golden files. Edit the affected golden file inputs to reflect
the new schema and regenerate the goldens using:

```bash
$ make refresh-goldens
```

