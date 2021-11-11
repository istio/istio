# How To Use Galley Test Data Set

## Some of this information is out-of-date.  It is preserved for reference, though the datasets may still be used

The Galley Test Data is designed to tests Galley from an inputs/outputs
perspective. It uses an embedded set of input and golden files from which
tests are calculated and executed.

The general directory/file structure is as follows:

```plain
# Area specific test data set.
galley/testdatasets/<area>/dataset/...

# Custom entry-point code for the data set, in a given area.
galley/testdatasets/<area>/dataset.go

# Generated Go file for test assets
galley/testdatasets/<area>/dataset.gen.go
```

## Conversion Test Data

The Conversion test data has the following format:

```plain
# Input file for the test
.../dataset/**/<testname>.yaml

# Optional Mesh Config Input file
.../dataset/**/<testname>_meshconfig.yaml

# Required expected resources file.
.../dataset/**/<testname>_expected.json
```

Tests can be ignored by adding a .skip file

```plain
# Input file for the test
.../dataset/**/<testname>.yaml

# Optional file, indicating that the test should be skipped.
.../dataset/**/<testname>.skip
```

The test file structure also allows multiple stages:

```plain
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
  "collection": [
    {
      "Metadata": {
        "name": "output-resource-1"
      },
      "Body": {}
    },
    {
      "Metadata": {
        "name": "outout-resource-2"
      },
      "Body": {}
    }
  ]
}
```

To add a new test data fileset:
 1. Create a new, appropriately named folder under dataset.
 2. Create the input, expected, and (optionally) the mesh config files.
 3. Run `BUILD_WITH_CONTAINER=1 make go-gen` from the repository root.
