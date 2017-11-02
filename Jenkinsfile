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
    env.HUB = 'gcr.io/istio-testing'
    env.ARTIFACTS_DIR = "gs://istio-artifacts/proxy/${GIT_SHA}/artifacts/debs"
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
      sh('make check')
    }
    bazel.updateBazelRc()
    stage('Bazel Build') {
      sh('make build')
    }
    stage('Bazel Tests') {
      sh('make test')
    }
    stage('Create and push artifacts') {
      sh('script/release-binary')
      sh('script/release-docker')
      sh('make artifacts')
    }
  }
}

def postsubmit(gitUtils, bazel, utils) {
  buildNode(gitUtils) {
    bazel.updateBazelRc()
    stage('Create and push artifacts') {
      sh('script/release-binary')
      sh('script/release-docker')
      sh('make artifacts')
    }
  }
}
