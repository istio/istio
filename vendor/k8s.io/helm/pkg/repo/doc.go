/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*Package repo implements the Helm Chart Repository.

A chart repository is an HTTP server that provides information on charts. A local
repository cache is an on-disk representation of a chart repository.

There are two important file formats for chart repositories.

The first is the 'index.yaml' format, which is expressed like this:

	apiVersion: v1
	entries:
	  frobnitz:
	  - created: 2016-09-29T12:14:34.830161306-06:00
		description: This is a frobniz.
		digest: 587bd19a9bd9d2bc4a6d25ab91c8c8e7042c47b4ac246e37bf8e1e74386190f4
		home: http://example.com
		keywords:
		- frobnitz
		- sprocket
		- dodad
		maintainers:
		- email: helm@example.com
		  name: The Helm Team
		- email: nobody@example.com
		  name: Someone Else
		name: frobnitz
		urls:
		- http://example-charts.com/testdata/repository/frobnitz-1.2.3.tgz
		version: 1.2.3
	  sprocket:
	  - created: 2016-09-29T12:14:34.830507606-06:00
		description: This is a sprocket"
		digest: 8505ff813c39502cc849a38e1e4a8ac24b8e6e1dcea88f4c34ad9b7439685ae6
		home: http://example.com
		keywords:
		- frobnitz
		- sprocket
		- dodad
		maintainers:
		- email: helm@example.com
		  name: The Helm Team
		- email: nobody@example.com
		  name: Someone Else
		name: sprocket
		urls:
		- http://example-charts.com/testdata/repository/sprocket-1.2.0.tgz
		version: 1.2.0
	generated: 2016-09-29T12:14:34.829721375-06:00

An index.yaml file contains the necessary descriptive information about what
charts are available in a repository, and how to get them.

The second file format is the repositories.yaml file format. This file is for
facilitating local cached copies of one or more chart repositories.

The format of a repository.yaml file is:

	apiVersion: v1
	generated: TIMESTAMP
	repositories:
	  - name: stable
	    url: http://example.com/charts
		cache: stable-index.yaml
	  - name: incubator
	    url: http://example.com/incubator
		cache: incubator-index.yaml

This file maps three bits of information about a repository:

	- The name the user uses to refer to it
	- The fully qualified URL to the repository (index.yaml will be appended)
    - The name of the local cachefile

The format for both files was changed after Helm v2.0.0-Alpha.4. Helm is not
backwards compatible with those earlier versions.
*/
package repo
