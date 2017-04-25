#!groovy

@Library('testutils@stable-bb42730')

import org.istio.testutils.Utilities
import org.istio.testutils.GitUtilities
import org.istio.testutils.Bazel

// Utilities shared amongst modules
def gitUtils = new GitUtilities()
def utils = new Utilities()
def bazel = new Bazel()

mainFlow(utils) {
  node {
    gitUtils.initialize() {
      // For automated qualification, update all the files to
      // use the version built from other module PRs.
      if (utils.getParam('SUBMODULES_UPDATE') != '') {
        sh('scripts/update_version.sh -Q')
      }
    }
  }
  if (utils.runStage('PRESUBMIT')) {
    presubmit(gitUtils, bazel, utils)
  }
}

def presubmit(gitUtils, bazel, utils) {
  defaultNode(gitUtils) {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Demo Test') {
      sh('tests/kubeTest.sh -g')
    }
  }
}
