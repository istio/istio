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
    stage('Smoke Test') {
      def logHost = 'stackdriver'
      def projID = utils.failIfNullOrEmpty(env.PROJECT)
      def e2eArgs = "--logs_bucket_path ${gitUtils.logsPath()} --log_provider=${logHost} --project_id=${projID} "
      sh("tests/e2e.sh ${e2eArgs}")
    }
  }
}

def smokeTest(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/istio') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    def logHost = 'stackdriver'
    def projID = utils.failIfNullOrEmpty(env.PROJECT)
    def e2eArgs = "--logs_bucket_path ${gitUtils.logsPath()} --log_provider=${logHost} --project_id=${projID} "
    if (utils.getParam('GITHUB_PR_HEAD_SHA') != '') {
      def prSha = utils.failIfNullOrEmpty(env.GITHUB_PR_HEAD_SHA)
      def prUrl = utils.failIfNullOrEmpty(env.GITHUB_PR_URL)
      def repo = prUrl.split('/')[4]
      def hub = 'gcr.io/istio-testing'
      switch (repo) {
        case 'pilot':
          def istioctlUrl = "https://storage.googleapis.com/istio-artifacts/${repo}/${prSha}/artifacts/istioctl"
          e2eArgs += "--pilot_hub=${hub}  " +
              "--pilot_tag=${prSha} " +
              "--istioctl_url=${istioctlUrl}"
          break
        case 'mixer':
          e2eArgs += "--mixer_hub=${hub}  " +
              "--mixer_tag=${prSha}"
          break
        default:
          break
      }
    }
    stage('Smoke Test') {
      sh("tests/e2e.sh ${e2eArgs}")
    }
  }
}
