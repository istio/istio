#!groovy

@Library('testutils@stable-41b0bf6')

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
  if (utils.runStage('SMOKE_TEST')) {
    smokeTest(gitUtils, bazel, utils)
  }
}

def presubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/istio') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Build and Checks') {
      sh('bin/linters.sh')
    }
    stage('Bazel Test') {
      bazel.test('//...')
    }
    stage('Demo Test') {
      sh("tests/kubeTest.sh")
    }
    stage('Smoke Test') {
        sh("tests/e2e.sh --logs_bucket_path ${gitUtils.logsPath()}")
    }
  }
}

def smokeTest(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/istio') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    def kubeTestArgs = ''
    def e2eArgs = "--logs_bucket_path ${gitUtils.logsPath()} "
    if (utils.getParam('GITHUB_PR_HEAD_SHA') != '') {
      def prSha = utils.failIfNullOrEmpty(env.GITHUB_PR_HEAD_SHA)
      def prUrl = utils.failIfNullOrEmpty(env.GITHUB_PR_URL)
      def repo = prUrl.split('/')[4]
      def hub = 'gcr.io/istio-testing'
      switch (repo) {
        case 'pilot':
          def istioctlUrl = "https://storage.googleapis.com/istio-artifacts/${repo}/${prSha}/artifacts/istioctl"
          kubeTestArgs = "-m ${hub},${prSha} " +
              "-i ${istioctlUrl}"
          e2eArgs += "--pilot_hub=${hub}  " +
              "--pilot_tag=${prSha} " +
              "--istioctl_url=${istioctlUrl}"
          break
        case 'mixer':
          kubeTestArgs = "-x ${hub},${prSha}"
          e2eArgs += "--mixer_hub=${hub}  " +
              "--mixer_tag=${prSha}"
          break
        default:
          break
      }
    }
    stage('Demo Test') {
      sh("tests/kubeTest.sh ${kubeTestArgs}")
    }
    stage('Smoke Test')
    sh("tests/e2e.sh ${e2eArgs}")
  }
}
