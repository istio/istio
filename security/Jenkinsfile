#!groovy

@Library('testutils@stable-2d6eb00')

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

  if (utils.runStage('POSTSUBMIT')) {
    postsubmit(gitUtils, bazel, utils)
  }
}

def postsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/auth') {
    bazel.updateBazelRc()
    stage('Docker Push') {
      def image = 'istio-ca'
      def tags = "${gitUtils.GIT_SHORT_SHA},\$(date +%Y-%m-%d-%H.%M.%S),latest"
      utils.publishDockerImages(image, tags)
    }
  }
}

def presubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/auth') {
    bazel.updateBazelRc()
    stage('Bazel Build') {
      sh('bin/install-prereqs.sh')
      bazel.fetch('-k //...')
      bazel.build('//...')
    }
    stage('Go Build') {
      sh('bin/setup.sh')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Code Check') {
      sh('bin/linters.sh')
      sh('bin/headers.sh')
    }
    stage('Code Coverage') {
      sh('bin/coverage.sh')
      utils.publishCodeCoverage('AUTH_CODECOV_TOKEN')
    }
  }
}
