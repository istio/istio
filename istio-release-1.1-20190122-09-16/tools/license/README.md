# Istio License Generation Guide
## Usage
Note: This tool requires https://github.com/benbalter/licensee for --summary and --match_detail to work.
The --branch flag is only used in generating links to licenses in the appropriate branch in istio/istio.
Licenses under manual_append have been manually copied and OK'd (usually because the package source only 
contains a link). 
#### Generate complete dump of every license, suitable for including in release build/binary image:
      go run get_dep_licenses.go --branch release-1.0.1
#### CSV format output with one package per line:
      go run get_dep_licenses.go --summary --branch release-1.0.1
#### Detailed info about how closely each license matches official text:
      go run get_dep_licenses.go --match-detail --branch release-1.0.1
#### Use a different branch from the current one. Will do git checkout to that branch and back to the current on completion. This can only be used from inside Istio repo:
      go run get_dep_licenses.go --branch release-1.0.1 --checkout
#### Check if all licenses are Google approved. Outputs lists of restricted, reciprocal, missing, and unknown status licenses.
      go run get_dep_licenses.go --check