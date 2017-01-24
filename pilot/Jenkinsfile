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

node('master') {
  try {
      goBuildNode(gitUtils, 'istio.io/manager') {
        def success = true
        utils.updatePullRequest('run')
        try {
          presubmit(bazel)
        } catch (Exception e) {
          success = false
          throw e
        } finally {
          utils.updatePullRequest('verify', success)
        }
      }
  } catch (Exception e) {
    currentBuild.result = 'FAILURE'
    throw e
  } finally {
    utils.sendNotification(gitUtils.NOTIFY_LIST)
  }
}

def presubmit(bazel) {
  bazel.updateBazelRc()
  stage('Bazel Build') {
    sh('touch platform/kube/config')
    bazel.fetch('-k //...')
    bazel.build('//...')
  }
  stage('Go Build') {
    sh('bin/init.sh')
  }
  stage('Bazel Tests') {
    bazel.test('//...')
  }
  stage('Code Check') {
    sh('bin/check.sh')
  }
}
