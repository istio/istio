#!groovy

@Library('testutils@stable-cd138c4')

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
      sh('bin/codecov.sh')
      utils.publishCodeCoverage('MIXER_CODECOV_TOKEN')
    }
    stage('Docker Test Push') {
      def images = 'mixer'
      def tags = env.GIT_SHA
      utils.publishDockerImagesToContainerRegistry(images, tags)
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
      utils.publishDockerImagesToContainerRegistry(images, tags)
    }
  }
}

def stablePostsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/mixer') {
    bazel.updateBazelRc()
    stage('Docker Push') {
      def date = new Date().format("YYYY-MM-dd-HH.mm.ss")
      def images = 'mixer,mixer_debug'
      def tags = "${env.GIT_SHORT_SHA},${date},latest"
      utils.publishDockerImagesToDockerHub(images, tags)
      utils.publishDockerImagesToContainerRegistry(images, tags, '', 'gcr.io/istio-io')
    }
  }
}
