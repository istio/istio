---
title: Staging Docs
headline: Staging Doc Changes
sidenav: doc-side-home-nav.html
bodyclass: docs
layout: docs
type: markdown
---

This page shows how to stage content that you want to contribute
to the Istio documentation.

## Before you begin

Create a fork of the Istio documentation repository as described in
[Creating a Doc Pull Request]({{site.baseurl}}/docs/home/contribute/creating-a-pull-request.html).

## Staging from your GitHub account

GitHub provides staging of content in your master branch. Note that you
might not want to merge your changes into your master branch. If that is
the case, choose another option for staging your content.

1. In your GitHub account, in your fork, merge your changes into
the master branch.

1. Change the name of your repository to `<your-username>.github.io`, where
`<your-username>` is the username of your GitHub account.

1. Delete the `CNAME` file.

1. View your staged content at this URL:

        https://<your-username>.github.io

## Staging locally

1. [Install Ruby 2.2 or later](https://www.ruby-lang.org){: target="_blank"}.

1. [Install RubyGems](https://rubygems.org){: target="_blank"}.

1. Verify that Ruby and RubyGems are installed:

        gem --version

1. Install the GitHub Pages package, which includes Jekyll:

        gem install github-pages

1. Clone your fork to your local development machine.

1. In the root of your cloned repository, enter this command to start a local
web server:

        jekyll serve

1. View your staged content at
[http://localhost:4000](http://localhost:4000){: target="_blank"}.

<i>NOTE: If you do not want Jekyll to interfere with your other globally installed gems, you can use `bundler`:</i> 
 
 	gem install bundler
 	bundle install
 	bundler exec jekyll serve

<i> Regardless of whether you use `bundler` or not, your copy of the site will then be viewable at: [http://localhost:4000](http://localhost:4000)</i>
