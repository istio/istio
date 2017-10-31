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
  if (utils.runStage('RELEASE')) {
    release(gitUtils, bazel)
  }
}

def presubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/auth') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Install required executables') {
      sh('bin/install-prereqs.sh')
    }
    stage('Check BUILD files') {
      sh('bin/check_build_files.sh')
    }
    stage('Bazel Build') {
      bazel.fetch('-k //...')
      bazel.build('//...')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Go Build') {
      sh('bin/setup.sh')
    }
    stage('Code Check') {
      sh('bin/linters.sh')
      sh('bin/headers.sh')
    }
    stage('Code Coverage') {
      sh('bin/codecov.sh | tee codecov.report')
      sh('bin/toolbox/presubmit/pkg_coverage.sh')
      utils.publishCodeCoverage('AUTH_CODECOV_TOKEN')
    }
    stage('Integration Test') {
      timeout(15) {
        sh("bin/e2e.sh --tag ${env.GIT_SHA} --hub gcr.io/istio-testing")
      }
    }
  }
}

def postsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/auth') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Bazel Build') {
      bazel.build('//...')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Go Build') {
      sh('bin/setup.sh')
    }
    stage('Code Coverage') {
      sh('bin/codecov.sh | tee codecov.report')
      sh('bin/toolbox/presubmit/pkg_coverage.sh')
      utils.publishCodeCoverage('AUTH_CODECOV_TOKEN')
    }
    stage('Integration Test') {
      timeout(15) {
        sh("bin/e2e.sh --tag ${env.GIT_SHA} --hub gcr.io/istio-testing")
      }
    }
  }
}

def release(gitUtils, bazel) {
  goBuildNode(gitUtils, 'istio.io/auth') {
    bazel.updateBazelRc()
    stage('Docker Push') {
      def tags = "${env.GIT_SHORT_SHA},${env.ISTIO_VERSION}-${env.GIT_SHORT_SHA}"
      def hubs = 'gcr.io/istio-io,docker.io/istio'
      if (env.GIT_TAG != '') {
        if (env.GIT_TAG == env.ISTIO_VERSION) {
          // Retagging
          tags = "${env.ISTIO_VERSION},${env.ISTIO_MINOR_VERSION}"
        } else {
          tags += ",${env.GIT_TAG}"
        }
      }
      sh("bin/push-docker.sh -h ${hubs} -t ${tags}")
    }
  }
}
