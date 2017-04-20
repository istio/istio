#!groovy

@Library('testutils@stable-838b134')

import org.istio.testutils.Utilities
import org.istio.testutils.GitUtilities
import org.istio.testutils.Bazel

// Utilities shared amongst modules
def gitUtils = new GitUtilities()
def utils = new Utilities()
def bazel = new Bazel()

node {
  gitUtils.initialize()
  bazel.setVars()
}

mainFlow(utils) {
  if (utils.runStage('PRESUBMIT')) {
    pullRequest(utils) {
      presubmit(gitUtils, bazel)
    }
  }
}

def presubmit(gitUtils, bazel) {
  defaultNode(gitUtils) {
    stage('Code Check') {
      sh('script/check-license-headers')
      sh('script/check-style')
    }
  }
  stage('Bazel Tests') {
    def branches = [
        'default': {
          buildNode(gitUtils) {
            bazel.test('//...')
          }
        },
        'asan'   : {
          buildNode(gitUtils) {
            bazel.test('--config=asan //...')
          }
        }
    ]
    parallel(branches)
  }
}

