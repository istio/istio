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
  goBuildNode(gitUtils, 'istio.io/istio') {
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
      def e2eArgs = "-b ${gitUtils.logsPath()} "
      if (utils.getParam('GITHUB_PR_HEAD_SHA') != '') {
        def prSha = utils.failIfNullOrEmpty(env.GITHUB_PR_HEAD_SHA)
        def prUrl = utils.failIfNullOrEmpty(env.GITHUB_PR_URL)
        def repo = prUrl.split('/')[4]
        def hub = 'gcr.io/istio-testing'
        switch (repo) {
          case 'manager':
            def istioctlUrl = "https://storage.googleapis.com/istio-artifacts/${prSha}/artifacts/istioctl"
            kubeTestArgs = "-m ${hub},${prSha} " +
                "-i ${istioctlUrl}"
            e2eArgs += "--manager_hub=${hub}  " +
                "--manager_tag=${prSha} " +
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
      sh("tests/kubeTest.sh ${kubeTestArgs}")
      sh("tests/kubeTest.sh ${e2eArgs}")

    }
  }
}
