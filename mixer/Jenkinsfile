#!groovy

@Library('testutils')

import org.istio.testutils.Utilities
import org.istio.testutils.GitUtilities
import org.istio.testutils.Bazel

// Utilities shared amongst modules
def gitUtils = new GitUtilities()
def utils = new Utilities()
def bazel = new Bazel()

mainFlow(utils) {
  pullRequest(utils) {

    node {
      gitUtils.initialize()
      bazel.setVars()
    }

    if (utils.runStage('PRESUBMIT')) {
      presubmit(gitUtils, bazel, utils)
    }

    if (utils.runStage('POSTSUBMIT')) {
      postsubmit(gitUtils, bazel, utils)
    }
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
      def tags = gitUtils.GIT_SHA
      utils.publishDockerImagesToContainerRegistry(images, tags)
    }
  }
}

def postsubmit(gitUtils, bazel, utils) {
  buildNode(gitUtils) {
    stage('Docker Push') {
      bazel.updateBazelRc()
      def images = 'mixer,mixer_debug'
      def tags = "${gitUtils.GIT_SHORT_SHA},\$(date +%Y-%m-%d-%H.%M.%S),latest"
      utils.publishDockerImages(images, tags)
    }
  }
}
