# Istio License Generation Guide
## Usage
Note: This tool requires https://github.com/benbalter/licensee for --summary and --match_detail to work.
#### Generate complete dump of every license, suitable for including in release build/binary image:
      go run get_dep_licenses.go
#### CSV format output with one package per line:
      go run get_dep_licenses.go --summary
#### Detailed info about how closely each license matches official text:
      go run get_dep_licenses.go --match-detail
#### Use a different branch from the current one. Will do git checkout to that branch and back to the current on completion. This can only be used from inside Istio repo:
      go run get_dep_licenses.go --branch release-0.8
