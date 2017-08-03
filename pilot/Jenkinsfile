#!groovy

@Library('testutils@stable-41b0bf6')

import org.istio.testutils.Utilities
import org.istio.testutils.GitUtilities
import org.istio.testutils.Bazel

// Utilities shared amongst modules
def gitUtils = new GitUtilities()
def utils = new Utilities()
def bazel = new Bazel()

// This should be updated for a release branch.
ISTIO_VERSION_URL = 'https://raw.githubusercontent.com/istio/istio/master/istio.RELEASE'


def setVersions() {
  def version = sh(returnStdout: true, script: "curl ${ISTIO_VERSION_URL}").trim()
  if (!(version  ==~ /[0-9]+\.[0-9]+\.[0-9]+/)) {
    error('Could not parse version')
  }
  def v = version.tokenize('.')
  env.ISTIO_VERSION = version
  env.ISTIO_MINOR_VERSION = "${v[0]}.${v[1]}"
}


mainFlow(utils) {
  node {
    setVersions()
    gitUtils.initialize()
    bazel.setVars()
  }
  // PR on master branch
  if (utils.runStage('PRESUBMIT')) {
    presubmit(gitUtils, bazel, utils)
  }
  // Postsubmit from master branch
  if (utils.runStage('POSTSUBMIT')) {
    postsubmit(gitUtils, bazel, utils)
  }
  // PR from master to stable branch for qualification
  if (utils.runStage('STABLE_PRESUBMIT')) {
    stablePresubmit(gitUtils, bazel, utils)
  }
  // Postsubmit form stable branch, post qualification
  if (utils.runStage('STABLE_POSTSUBMIT')) {
    stablePostsubmit(gitUtils, bazel, utils)
  }
  // Regression test to run for modules managed depends on
  if (utils.runStage('REGRESSION')) {
    regression(gitUtils, bazel, utils)
  }
}

def presubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/pilot') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    sh('ln -s ~/.kube/config platform/kube/')
    stage('Bazel Build') {
      // Use Testing cluster
      sh('bin/install-prereqs.sh')
      bazel.fetch('-k //...')
      bazel.build('//...')
    }
    stage('Build istioctl') {
      def remotePath = gitUtils.artifactsPath('istioctl')
      sh("bin/upload-istioctl -p ${remotePath}")
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
    stage('Code Coverage') {
      sh('bin/codecov.sh | tee codecov.report')
      sh('bin/toolbox/pkg_coverage.sh')
      utils.publishCodeCoverage('PILOT_CODECOV_TOKEN')
    }
    stage('Integration Tests') {
      timeout(15) {
        sh("bin/e2e.sh -tag ${env.GIT_SHA}")
      }
    }
  }
}

def stablePresubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/pilot') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    sh('ln -s ~/.kube/config platform/kube/')
    stage('Build istioctl') {
      def remotePath = gitUtils.artifactsPath('istioctl')
      sh("bin/upload-istioctl -p ${remotePath}")
    }
    stage('Integration Tests') {
      timeout(60) {
        sh("bin/e2e.sh -count 10 -logs=false -tag ${env.GIT_SHA}")
      }
    }
  }
}

def stablePostsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/pilot') {
    bazel.updateBazelRc()
    sh('touch platform/kube/config')
    stage('Build istioctl') {
      def remotePath = gitUtils.artifactsPath('istioctl')
      sh("bin/upload-istioctl -p ${remotePath}")
    }
    stage('Docker Push') {
      def tags = "${env.GIT_SHORT_SHA},${env.ISTIO_VERSION}-${env.GIT_SHORT_SHA}"
      if (env.GIT_TAG != '') {
        if (env.GIT_TAG == env.ISTIO_VERSION) {
          // Retagging
          tags = "${env.ISTIO_VERSION},${env.ISTIO_MINOR_VERSION}"
        } else {
          tags += ",${env.GIT_TAG}"
        }
      }
      sh("bin/push-docker -tag ${tags} -hub gcr.io/istio-io")
    }
  }
}

def postsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/pilot') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    sh('ln -s ~/.kube/config platform/kube/')
    stage('Code Coverage') {
      sh('bin/install-prereqs.sh')
      bazel.test('//...')
      sh('bin/init.sh')
      sh('bin/codecov.sh | tee codecov.report')
      sh('bin/toolbox/pkg_coverage.sh')
      utils.publishCodeCoverage('PILOT_CODECOV_TOKEN')
    }
    utils.fastForwardStable('pilot')
  }
}

def regression(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/pilot') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Bazel Build') {
      // Use Testing cluster
      sh('ln -s ~/.kube/config platform/kube/')
      bazel.fetch('-k //...')
      bazel.build('//...')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Integration Tests') {
      timeout(15) {
        sh("bin/e2e.sh -tag ${env.GIT_SHA}")
      }
    }
  }
}
