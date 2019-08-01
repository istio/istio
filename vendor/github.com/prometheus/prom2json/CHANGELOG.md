## master / unreleased

### **Breaking changes**

### Changes

* [CHANGE]
* [FEATURE]
* [ENHANCEMENT]
* [BUGFIX]

## 1.2.2 / 2019-07-23

* [FEATURE] Add ARM container images. #61
* [BUGFIX] Properly set the sum in a histogram. #65

## 1.2.1 / 2019-05-20

_No actual code changes. Only a fix of the CircleCI config to make Docker
images available again._

* [BUGFIX] Fix image upload to Docker Hub and Quay.

## 1.2.0 / 2019-05-17

### **Breaking changes**

Users of the `prom2json` package have to take into account that the interface
of `FetchMetricFamilies` has changed (to allow the bugfix below). For users of
the command-line tool `prom2json`, this is just an internal change without any
external visibility.

### Changes

* [FEATURE] Support timestamps.
* [BUGFIX] Do not create a new Transport for each call of `FetchMetricFamilies`.

## 1.1.0 / 2018-12-09

* [FEATURE] Allow reading from STDIN and file (in addition to URL).
* [ENHANCEMENT] Support Go modules for dependency management.
* [ENHANCEMENT] Update dependencies.

## 1.0.0 / 2018-10-20

Initial release
