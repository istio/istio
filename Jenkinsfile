#!groovy

@Library('testutils@stable-983183f')

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
    bazel.setVars()
  }
  if (utils.runStage('PRESUBMIT')) {
    presubmit(gitUtils, bazel, utils)
  }
}

def presubmit(gitUtils, bazel, utils) {
  defaultNode(gitUtils) {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Bazel Build') {
      bazel.build('//...')
    }
    stage('Code Check') {
      sh('bin/linters.sh')
    }
    stage('Bazel Test') {
      bazel.test('//...')
    }
    stage('Demo Test') {
      def kubeTestArgs = ''
      if (utils.getParam('GITHUB_PR_HEAD_SHA') != '') {
        def prSha = utils.failIfNullOrEmpty(env.GITHUB_PR_HEAD_SHA)
        def prUrl = utils.failIfNullOrEmpty(env.GITHUB_PR_URL)
        def repo = prUrl.split('/')[4]
        switch (repo) {
          case 'manager':
            kubeTestArgs = "-m gcr.io/istio-testing,${prSha} " +
                "-i https://storage.googleapis.com/istio-artifacts/${prSha}/artifacts/istioctl"
            break
          case 'mixer':
            kubeTestArgs = "-x gcr.io/istio-testing,${prSha}"
            break
          default:
            break
        }
      }
      sh("tests/kubeTest.sh ${kubeTestArgs}")
    }
  }
}
