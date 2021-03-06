---
description: |
    The file Packer builder is not really a builder, it just creates an artifact
    from a file. It can be used to debug post-processors without incurring high
    wait times.
layout: docs
page_title: 'File - Builders'
sidebar_current: 'docs-builders-file'
---

# File Builder

Type: `file`

The `file` Packer builder is not really a builder, it just creates an artifact
from a file. It can be used to debug post-processors without incurring high
wait times.

## Basic Example

Below is a fully functioning example. It create a file at `target` with the
specified `content`.

``` json
{
  "type": "file",
  "content": "Lorem ipsum dolor sit amet",
  "target": "dummy_artifact"
}
```

## Configuration Reference

Configuration options are organized below into two categories: required and
optional. Within each category, the available options are alphabetized and
described.

Any [communicator](/docs/templates/communicator.html) defined is ignored.

### Required:

-   `target` (string) - The path for a file which will be copied as the
    artifact.

### Optional:

You can only define one of `source` or `content`. If none of them is defined
the artifact will be empty.

-   `source` (string) - The path for a file which will be copied as the
    artifact.

-   `content` (string) - The content that will be put into the artifact.
