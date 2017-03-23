#!groovy

@Library('testutils@stable-afad32f')

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
