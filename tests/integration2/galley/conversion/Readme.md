**How To Use Galley Conversion Tests**

The Galley Conversion Tests are designed to tests Galley from an inputs/outputs
perspective. It uses an embedded set of input and golden files from which
tests are calculated and executed.

The directory structure is as follows:

```
# Top Level Folder for the Test
conversion/

# The main driver code for the test
conversion/conversion_test.go
conversion/main_test.go

# Go Library that contains the test data.
conversion/testdata

# The actual dataset folder
conversion/testdata/dataset
```

Typically, a set of files needed for a test is as follows:

```
# Input file for the test
.../dataset/**/<testname>.yaml

# Optional Mesh Config Input file
.../dataset/**/<testname>_meshconfig.yaml

# Required expected resources file.
.../dataset/**/<testname>_expected.json
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

To add a new test:
 1. Create a new, appropriately named folder under dataset.
 2. Create the input, expected, and (optionally) the mesh config files.
 3. Call ```go generate``` on dataset.go



