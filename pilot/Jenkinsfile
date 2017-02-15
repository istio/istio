#!groovy

@Library('testutils')

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
    def success = true
    utils.updatePullRequest('run')
    try {
      presubmit(gitUtils, bazel)
    } catch (Exception e) {
      success = false
      throw e
    } finally {
      utils.updatePullRequest('verify', success)
    }
  }
}

def presubmit(gitUtils, bazel) {
  goBuildNode(gitUtils, 'istio.io/manager') {
    bazel.updateBazelRc()
    stage('Bazel Build') {
      sh('touch platform/kube/config')
      bazel.fetch('-k //...')
      bazel.build('//...')
    }
    stage('Go Build') {
      sh('bin/init.sh')
    }
    stage('Code Check') {
      sh('bin/check.sh')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Integration Tests') {
      sh('bin/e2e.sh -t alpha-' + gitUtils.GIT_SHA + ' -d -c ""')
    }
  }
}
