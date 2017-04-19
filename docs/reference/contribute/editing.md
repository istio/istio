---
title: Editing Docs
headline: Editing Istio Docs
sidenav: doc-side-home-nav.html
bodyclass: docs
layout: docs
type: markdown
---

<script language="JavaScript">
var forwarding=window.location.hash.replace("#","");
$( document ).ready(function() {
    if(forwarding) {
        $("#continueEdit").show();
        $("#continueEditButton").text("Edit " + forwarding);
        $("#continueEditButton").attr("href", "https://github.com/istio/istio.github.io/edit/master/" + forwarding)
        $("#viewOnGitHubButton").text("View " + forwarding + " on GitHub");
        $("#viewOnGitHubButton").attr("href", "https://github.com/istio/istio.github.io/tree/master/" + forwarding)
    } else {
        $("#continueEdit").hide();
    }
});
</script>

<div id="continueEdit">
    <h2>Edit a Single Page</h2>

    <p>
        Click the button below to edit the page you were just on. When you are done, click <b>Commit Changes</b> at the bottom of the
        screen. This creates a copy of our site in your GitHub account called a <i>fork</i>. You can make other changes in your fork
        after it is created, if you want. When you are ready to send us all your changes, go to the index page for your fork and click
        <b>New Pull Request</b> to let us know about it.
    </p>

    <p><button id="continueEditButton" class="btn btn-grpc waves-effect waves-light" /></p>
    <p><br/></p>
    <p><button id="viewOnGitHubButton" class="btn btn-grpc waves-effect waves-light" /></p>
</div>

<h2>Edit the Whole Site</h2>
<p>
    Click the button below to visit the GitHub repository for this whole web site. You can then click the
    <b>Fork</b> button in the upper-right area of the screen to 
    create a copy of our site in your GitHub account called a <i>fork</i>. Make any changes you want in your fork, and when you
    are ready to send those changes to us, go to the index page for your fork and click <b>New Pull Request</b> to let us know about it.
</p>

<p><a class="btn btn-grpc waves-effect waves-light" href="https://github.com/istio/istio.github.io/">Browse this site's source code</a></p>
