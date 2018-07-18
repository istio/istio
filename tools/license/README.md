# Istio License Generation Guide
## Usage
#### Generate CSV format output with one package per line:
      go run get_dep_licenses.go --branch release-0.8 --summary
#### Generate complete dump of every license, suitable for including in release build/binary image:
      go run get_dep_licenses.go --branch release-0.8
#### Generate detailed info about how closely each license matches official text:
      go run get_dep_licenses.go --branch release-0.8 --match-detail

