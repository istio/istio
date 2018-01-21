
# What's this for?

protoc-gen-docs is a plugin for the Google protocol buffer compiler to generate
documentation in for any given input protobuf. Run it by building this program and
putting it in your path with the name `protoc-gen-docs`.
That word `docs` at the end becomes part of the option string set for the
protocol compiler, so once the protocol compiler (protoc) is installed
you can run

```bash
protoc --docs_out=output_directory input_directory/file.proto
```

to generate a page of HTML describing the protobuf defined by file.proto.
With that input, the output will be written to

	output_directory/file.pb.html

Using the `mode` option, you can control the output format from the plugin. The
html_page option is the default and produces a fully self-contained HTML page.
The html_fragment option outputs an HTML fragment that can be used to embed in a
larger page. Finally, the jekyll_html option outputs an HTML fragment augmented
with [Jekyll front-matter](https://jekyllrb.com/docs/frontmatter/)

You specify the mode option using this syntax:

```bash
protoc --docs_out=mode=html_page:output_directory input_directory/file.proto
```

Using the `warnings` option, you can control whether warnings are produced
to report proto elements that aren't commented. You can use this option with
the following syntax:

```bash
protoc --docs_out=warnings=true:output_directory input_directory/file.proto
```

You can specify both the mode and warnings options by separating them with commas:

```bash
protoc --docs_out=warnings=true,mode=html_page:output_directory input_directory/file.proto
```

# Writing Docs

Writing documentation for use with protoc-gen-docs is simply a matter of adding comments to elements
within the input proto file. You can put comments directly above individual elements, or to the
right. For example:

```proto
// A package-level comment
package pkg;

// This documents the message as a whole
message MyMsg {
    // This documents this field 
    // It can contain many lines.
    int32 field1 = 1;

    int32 field2 = 2;       // This documents field2
}
```

Comments are treated as markdown. You can thus embed classic markdown annotations within any comment.

## Linking to types

In addition to normal markdown links, you can also use special type links within any comment. Type
links are used to create a link to other types within the set of protos. You specify type links using
two pairs of square brackets such as:

```proto

// This is a comment that links to another type: [MyOtherType][MyPkg.MyOtherType]
message MyMsg {

}

```

The first square brackets contain the name of the type to display in the resulting documentation. The second
square brackets contain the fully qualified name of the type being referenced, including the
package name.

## Annotations

Within a proto file, you can insert special comments which provide additional metadata to
use in producing quality documentation. Within a package, optionally include an unattached
comment of the form:

```
// $title: My Title
// $overview: My Overview
// $location: https://mysite.com/mypage.html
```

`$title` provides a title for the generated package documentation. This is used for things like the
title of the generated HTML. `$overview` is a one-line description of the package, useful for
tables of contents or indexes. Finally, `$location` indicates the expected URL for the generated
documentation. This is used to help downstream processing tools to know where to copy
the documentation, and is used when creating documentation links from other packages to this one.

If a comment for proto fields, enums, or methods contains the annotation `$hide_from_docs`,
then the associated element will be omitted from the output. This is useful when staging the
introduction of new features that aren't quite ready for use yet.
