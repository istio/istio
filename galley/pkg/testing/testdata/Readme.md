**How To Use Galley Test Data Set**

The Galley Test Data is designed to tests Galley from an inputs/outputs
perspective. It uses an embedded set of input and golden files from which
tests are calculated and executed.

The directory structure is as follows:

```
# Input file for the test
.../dataset/**/<testname>.yaml

# Optional Mesh Config Input file
.../dataset/**/<testname>_meshconfig.yaml

# Required expected resources file.
.../dataset/**/<testname>_expected.json
```

Tests can be ignored by adding a .skip file

```
# Input file for the test
.../dataset/**/<testname>.yaml

# Optional file, indicating that the test should be skipped.
.../dataset/**/<testname>.skip
```

The test file structure also allows multiple stages:
```
# Input file for the test
.../dataset/**/<testname>_<stageNo>.yaml
.../dataset/**/<testname>_<stageNo>_meshconfig.yaml
.../dataset/**/<testname>_<stageNo>_expected.json

e.g.
# First stage files. Meshconfig carries over to the next stage
.../dataset/**/foo_0.yaml
.../dataset/**/foo_0_meshconfig.yaml
.../dataset/**/foo_0_expected.json
# Second stage files.
.../dataset/**/foo_1.yaml
.../dataset/**/foo_1_expected.json

```


The expected file structure is as follows:

```json
{
  "type URL 1": [
    {
      "Metadata": {
        "name": "output-resource-1"
      },
      "Resource": {}
    },
    {
      "Metadata": {
        "name": "outout-resource-2"
      },
      "Resource": {}
    }
  ]
}
```

To add a new test data fileset:
 1. Create a new, appropriately named folder under dataset.
 2. Create the input, expected, and (optionally) the mesh config files.
 3. Call ```go generate``` on dataset.go



