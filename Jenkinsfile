#!groovy

@Library('testutils@stable-0ee5fd9')

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
    // Proxy does build work correctly with Hazelcast.
    // Must use .bazelrc.jenkins
    bazel.setVars('', '')
  }
  if (utils.runStage('PRESUBMIT')) {
    presubmit(gitUtils, bazel)
  }
  if (utils.runStage('POSTSUBMIT')) {
    postsubmit(gitUtils, bazel, utils)
  }
}

def presubmit(gitUtils, bazel) {
  buildNode(gitUtils) {
    stage('Code Check') {
      sh('script/check-license-headers')
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
    stage('Push Debian Package') {
      sh "script/push-debian.sh -c dbg -p gs://istio-artifacts/proxy/${GIT_SHA}/artifacts/debs"
    }
  }
}

def postsubmit(gitUtils, bazel, utils) {
  buildNode(gitUtils) {
    bazel.updateBazelRc()
    stage('Binary push') {
      sh 'script/release-binary'
    }
    stage('Docker Push') {
      sh 'script/release-docker'
    }
    stage('Push Debian Package') {
      sh "script/push-debian.sh -c dbg -p gs://istio-artifacts/proxy/${GIT_SHA}/artifacts/debs"
    }
  }
}
