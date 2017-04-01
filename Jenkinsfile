#!groovy

@Library('testutils@stable-3e4d089')

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

def postsubmit(gitUtils, bazel, utils) {
  buildNode(gitUtils) {
    bazel.updateBazelRc()
    stage('Push Binary') {
      sh 'script/release-binary'
    }
    stage('Docker Push') {
      def images = 'proxy,proxy_debug'
      def tags = "${gitUtils.GIT_SHORT_SHA},\$(date +%Y-%m-%d-%H.%M.%S),latest"
      utils.publishDockerImages(images, tags, 'release')
    }
  }
}
