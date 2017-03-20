#!groovy

@Library('testutils@stable-afad32f')

import org.istio.testutils.Utilities
import org.istio.testutils.GitUtilities
import org.istio.testutils.Bazel

// Utilities shared amongst modules
def gitUtils = new GitUtilities()
def utils = new Utilities()
def bazel = new Bazel()

mainFlow(utils) {
  node {
    gitUtils.initialize()
  }
  if (utils.runStage('PRESUBMIT')) {
    presubmit(gitUtils, bazel, utils)
  }
}

def presubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/istio') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Demo Test') {
      sh('tests/kubeTest.sh')
    }
  }
}
