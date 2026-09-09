# ADR-0002. Reviewing code, and why not yet

Date: 2026-09-09
Status: accepted

## Context

The tool was built to review markdown, but nothing in it is markdown-specific:
anchors are quoted text, the sidecar is a list of comments against a file, and
the language server, the browser and the CLI never look at the file type.

Verified rather than assumed. Reviewing a Go file works today, with no change:

```
$ mdrev list server.go
e691f5ba  server.go:4  [issue]
    "json.Marshal(req)"
    Claude: Error dropped: a marshal failure sends an empty body.

$ mdrev apply --file server.go --id 39790779
Applied 39790779 at server.go:4
```

The language server publishes the same diagnostics for `.go`, and the reference
`mrsf validate` accepts a sidecar written against code, so the format stays
compatible outside markdown.

The use is real: an inline thread with an agent *before* the commit, in the
editor, on live code. Pull request review starts after the push and lives in a
browser.

## Decision

Keep the declared scope at markdown for now. Revisit after the extension is in
the Zed registry.

## Why not now

Three obstacles, in ascending cost:

**CriticMarkup does not work in code.** A `{>>note<<}` in a source file stops it
compiling, and every other language server complains until it is filed. Code
review would rely on the editor task and the browser instead. Accepting a marker
inside a language comment (`// {>>note<<}`) would fix it and is small, but is
not written.

**Sidecars beside sources litter a repository.** One `*.go.review.yaml` per
reviewed file. Either `init` writes a `.gitignore` entry, or storage moves under
`.mdrev/` — and that breaks both documents outside a project and the layout
other MRSF tools expect.

**The registry submission gets harder.** Declaring the extension a language
server for twenty languages invites exactly the question the publishing rules
raise about not misusing the extension API. Submitting a narrow, explainable
markdown extension first, and widening in a later version with something to
show, is the cheaper order.

## Consequences

- Reviewing code from the CLI works and is not documented as a feature. It is
  not prevented either.
- The name `mdrev` will read as too narrow if the scope widens. Renaming after
  the registry submission is expensive, so the name is a decision to make before
  widening, not after.
