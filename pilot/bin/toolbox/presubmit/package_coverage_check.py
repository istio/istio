from istio_test_infra.toolbox.pkgCheck import pkg_cc_check
import argparse
import sys

def packageCodeCoverageCheck(report, requirement):
    pkgcheck = pkg_cc_check.PkgChecker(report, requirement)
    return pkgcheck.check()

if __name__ == "__main__":
    parse = argparse.ArgumentParser()
    parse.add_argument("-cc_report", default="codecov.report", dest='cc_report',  help="Package code coverage report.")
    parse.add_argument("-cc_requirement", default="codecov.requirement", dest='cc_requirement', help="Package code coverage requuirement.")

    args = parse.parse_args()
    sys.exit(packageCodeCoverageCheck(args.cc_report, args.cc_requirement))

