# ADR-0001. Review storage format, and what we did with existing tools

Date: 2026-09-09
Status: accepted

## Context

Markdown documents get discussed as often as code, but there is nothing to
review them with. We wanted the review mode of an office suite: inline comments
on a fragment, questions and answers, suggested edits with accept and reject,
and change tracking — reachable both by a human in an editor and by an agent
through a CLI.

The conditions we worked under:

- Documents live locally, often outside a project and outside git.
- Diagrams (mermaid) and tables must survive review untouched.
- The editor is Zed.
- Publishing the document to an external service is out of the question.

We looked at four existing approaches and took two of them seriously.

## Rejected outright

**Merge/pull requests.** Inline comments on diff lines exist and work, but you
review a diff rather than a document, and prose discussion pollutes the
repository history.

**Round-trip through pandoc to .docx.** Real track changes in Word, but the
conversion loses mermaid diagrams and mangles the markup on the way back.

## Sidemark / MRSF

[Markdown Review Sidecar Format](https://sidemark.org) keeps comments in a
separate file beside the document: `doc.md` plus `doc.md.review.yaml`.

Verified on a live document:

- The document is not modified at all; diagrams and tables are safe.
- Re-anchoring works: after six lines were inserted at the top of a document,
  `mrsf reanchor` found the comments by their text and restored not just the
  lines but the columns.
- The format is readable YAML with a schema, and `x_*` keys are reserved for
  extensions.
- It ships a CLI, an MCP server and a validator.

**Decision: adopt the format, not the tooling.**

What the tooling could not give us. Commenting is only possible inside VS Code,
Monaco or Milkdown — nothing for Zed, and no standalone viewer. The format does
not describe suggested edits with accept and reject. And editor integration
needs a long-running language server, which is not part of the distribution.

The format, however, carries the one genuinely hard part of this problem —
durable anchors and how they degrade — so reinventing it would have been
foolish. We read and write MRSF, and we check compatibility with someone
else's validator: `mrsf validate` accepts what mdrev produces. A suggested edit
is stored in `x_suggested_text`, that is, in the extension namespace the
specification sets aside, so other tools do not choke on it.

## md-redline

[md-redline](https://github.com/dejuknow/md-redline) is a browser viewer:
select text with the mouse, leave a comment; an MCP server hands the comments
to an agent. It is the closest to our goal in terms of UX, and we considered
forking it.

**Decision: neither adopt nor fork.**

Storage. A comment is written as an invisible HTML marker inside the `.md`
itself:

```
Service accepts <!-- @comment{"id":"6862dbfb-…","anchor":"raw",
"text":"why not wet?","author":"User",…} -->raw counterparty details
```

One short remark turns a line of prose into three hundred characters of JSON,
and that lands in the document's diff. The anchor is only the surrounding
context, with no re-anchoring: edit the paragraph and the attachment silently
drifts.

Isolation. On the very first trial the comment landed in a file we had not
opened: the viewer trusts the whole home directory by default and offers
navigation across it.

Forking. Storage is not isolated behind a seam.
`src/lib/comment-parser.ts` is 2061 lines, and almost every one of its 28
exports has the signature `(rawMarkdown: string, …) => string`. The premise
"a document is a string with comments inside it" is baked into the types, so
moving to a sidecar means rewriting the core: every export, `App.tsx` at 3517
lines, the server side, and around fifteen e2e specs that assert exactly the
presence of markers in the file. At 34k lines with an active upstream, that is
a permanent merge burden in exchange for someone else's UI.

## Decision

Our own tool in Go, which:

- stores reviews in an MRSF sidecar, leaving the document untouched;
- runs as a language server, so comments are visible in the editor and
  suggested edits apply in one action;
- offers a CLI with `--json`, through which an agent reads comments and replies.

One limitation we accepted knowingly: LSP is a one-way channel and cannot ask a
human for text. Comment input is done with editor tasks
(`ZED_SELECTED_TEXT`), not through the protocol.

## Consequences

- We depend on someone else's format specification. That is the deliberate
  price for working anchors; compatibility is held by testing against
  `mrsf validate`.
- `x_suggested_text` is our extension. If MRSF ever describes suggested edits
  itself, we move to its fields.
- Reading in a browser stays a second stage, and will be built on Milkdown
  rather than a fork of someone else's viewer.
