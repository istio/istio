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
}

def presubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/mixer') {
    bazel.updateBazelRc()
    stage('Bazel Build') {
      bazel.fetch('-k //...')
      bazel.build('//...')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Code Check') {
      sh('bin/linters.sh')
      sh('bin/racetest.sh')
    }
    stage('Code Coverage') {
      sh('bin/codecov.sh > codecov.report')
      sh('bazel-bin/bin/toolbox/presubmit/package_coverage_check')
      utils.publishCodeCoverage('MIXER_CODECOV_TOKEN')
    }
    stage('Docker Test Push') {
      def images = 'mixer'
      def tags = env.GIT_SHA
      // Docker images built with bazel
      utils.publishDockerImagesToContainerRegistry(images, tags)
      // Docker images built with docker
      sh("bin/publish-docker-images.sh -t ${tags} -h gcr.io/istio-testing")
    }
  }
}

def postsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/mixer') {
    bazel.updateBazelRc()
    stage('Code Coverage') {
      bazel.fetch('-k //...')
      bazel.build('//...')
      sh('bin/bazel_to_go.py')
      bazel.test('//...')
      sh('bin/codecov.sh')
      utils.publishCodeCoverage('MIXER_CODECOV_TOKEN')
    }
    utils.fastForwardStable('mixer')
  }
}

def stablePresubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/mixer') {
    bazel.updateBazelRc()
    stage('Docker Push') {
      def images = 'mixer'
      def tags = env.GIT_SHA
      // Docker images built with bazel
      utils.publishDockerImagesToContainerRegistry(images, tags)
      // Docker images built with docker
      sh("bin/publish-docker-images.sh -t ${tags} -h gcr.io/istio-testing")
    }
  }
}

def stablePostsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/mixer') {
    bazel.updateBazelRc()
    stage('Docker Push') {
      def images = 'mixer,mixer_debug'
      def tags = "${env.GIT_SHORT_SHA},${env.ISTIO_VERSION}-${env.GIT_SHORT_SHA}"
      if (env.GIT_TAG != '') {
        if (env.GIT_TAG == env.ISTIO_VERSION) {
          // Retagging
          tags = "${env.ISTIO_VERSION},${env.ISTIO_MINOR_VERSION}"
        } else {
            tags += ",${env.GIT_TAG}"
        }
      }
      // Docker images built with bazel
      utils.publishDockerImagesToDockerHub(images, tags)
      utils.publishDockerImagesToContainerRegistry(images, tags, '', 'gcr.io/istio-io')
      // Docker images built with docker
      sh("bin/publish-docker-images.sh -t ${tags} -h gcr.io/istio-io")
      withDockerRegistry([credentialsId: env.ISTIO_TESTING_DOCKERHUB]) {
        sh("bin/publish-docker-images.sh -t ${tags} -h docker.io/istio")
      }
    }
  }
}
