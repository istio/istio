# Revision deployer

## Overview

To create a new job with multiple revisions, create a JSON configuration file with the structure below and place in a new directory under `revision-deployer`.

```json
{
  "revisions": [
    {
      "name": "asm-citadel-add-mesh-ca-root",
      "ca": "CITADEL",
      "overlay": "./revision-deployer/meshca-migration/asm-citadel-add-mesh-ca-root.yaml",
      "scriptaro-flags": "--option add_mesh_ca_root"
    },
    {
      "name": "asm-mesh-ca",
      "ca": "MESH_CA",
      "overlay": "./revision-deployer/meshca-migration/asm-mesh-ca.yaml"
    }
  ]
}
```

Add custom `overlay` files for the revisions to the same directory and reference them from the configuration file.

When creating a `Prow` job, simply set the `--revision-config` flag to the path
of the configuration file and the specified revisions wil be deployed.
