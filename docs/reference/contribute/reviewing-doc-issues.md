---
title: Doc Issues
headline: Reviewing Doc Issues
sidenav: doc-side-home-nav.html
bodyclass: docs
layout: docs
type: markdown
---

This page explains how documentation issues are reviewed and prioritized for the
[istio/istio.github.io](https://github.com/istio/istio.github.io){: target="_blank"} repository.
The purpose is to provide a way to organize issues and make it easier to contribute to
Istio documentation. The following should be used as the standard way of prioritizing,
labeling, and interacting with issues.

## Categorizing issues

Issues should be sorted into different buckets of work using the following labels and definitions. If an issue
doesn't have enough information to identify a problem that can be researched, reviewed, or worked on (i.e. the
issue doesn't fit into any of the categories below) you should close the issue with a comment explaining why it
is being closed.

<table>
<tr>
    <td>Needs Clarification</td>
    <td><ul>
        <li>Issues that need more information from the original submitter to make them actionable. Issues with this label that aren't followed up within a week 
        may be closed.</li>
    </ul></td>
</tr>

<tr>
    <td>Actionable</td>
    <td><ul>
        <li>Issues that can be worked on with current information (or may need a comment to explain what needs to be done to make
        it more clear)</li>
        <li>Allows contributors to have easy to find issues to work on</li>
    </ul></td>
</tr>

<tr>
    <td>Needs Tech Review</td>
    <td><ul>
        <li>Issues that need more information in order to be worked on (the proposed solution needs to be proven, a subject matter expert needs to be involved, 
        work needs to be done to understand the problem/resolution and if the issue is still relevant)</li>
        <li>Promotes transparency about level of work 
        needed for the issue and that issue is in progress</li>
    </ul></td>
</tr>

<tr>
    <td>Needs Docs Review</td>
    <td><ul>
        <li>Issues that are suggestions for better processes or site improvements that require community agreement to be implemented</li>
        <li>Topics can be brought to SIG meetings as agenda items</li>
    </ul></td>
</tr>

<tr>
    <td>Needs UX Review</td>
    <td><ul>
        <li>Issues that are suggestions for improving the user interface of the site.</li>
        <li>Fixing broken site elements</li> 
    </ul></td>
</tr>
</table>

## Prioritizing Issues

The following labels and definitions should be used to prioritize issues. If you change the priority of an issues, please comment on
the issue with your reasoning for the change.

<table>
<tr>
    <td>P1</td>
    <td><ul>
        <li>Major content errors affecting more than 1 page</li>
        <li>Broken code sample on a heavily trafficked page</li>  
        <li>Errors on a “getting started” page</li>
        <li>Well known or highly publicized customer pain points</li>
        <li>Automation issues</li>
    </ul></td>
</tr>

<tr>
    <td>P2</td>
    <td><ul>
        <li>Default for all new issues</li>
        <li>Broken code for sample that is not heavily used</li>
        <li>Minor content issues in a heavily trafficked page</li>
        <li>Major content issues on a lower-trafficked page</li>
    </ul></td>
</tr>

<tr>
    <td>P3</td>
    <td><ul>
        <li>Typos and broken anchor links</li>
    </ul></td>
</tr>
</table>

## Handling special issue types

If a single problem has one or more issues open for it, the problem should be consolidated into a single issue. You should decide which issue to keep open 
(or open a new issue), port over all relevant information, link related issues, and close all the other issues that describe the same problem. Only having
a single issue to work on will help reduce confusion and avoid duplicating work on the same problem.

Depending on where a dead link is reported, different actions are required to resolve the issue. Dead links in the reference
docs are automation issues and should be assigned a P1 until the problem can be fully understood. All other dead links are issues
that need to be manually fixed and can be assigned a P3.
