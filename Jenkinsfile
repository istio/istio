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
  // Proxy does build work correctly with Hazelcast.
  // Must use .bazelrc.jenkins
  bazel.setVars('', '')
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
  if (utils.runStage('POSTSUBMIT')) {
    buildNode(gitUtils) {
      bazel.updateBazelRc()
      sh 'script/release-binary'
    }
  }
}

def presubmit(gitUtils, bazel) {
  buildNode(gitUtils) {
    stage('Code Check') {
      sh('script/check-style')
    }
    bazel.updateBazelRc()
    stage('Bazel Fetch') {
      bazel.fetch('-k //...')
    }
    stage('Bazel Build') {
      bazel.build('//...')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Push Test Binary') {
      sh 'script/release-binary'
    }
  }
}
