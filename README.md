# mdrev

Review mode for markdown documents: inline comments on a fragment of text,
questions and answers, suggested edits you can apply or dismiss.

Comments live in a separate file next to the document
([MRSF](https://sidemark.org)), so the `.md` itself never changes — diagrams,
tables and the git history stay clean.

```
doc.md              the document, never touched
doc.md.review.yaml  comments, threads, suggested edits
```

It runs as two front ends over the same store: a language server, so comments
show up right in the editor, and a CLI with `--json`, so an agent can review
the document too.

## Install

```sh
brew install esivres/tap/mdrev          # macOS and Linux
scoop bucket add esivres https://github.com/esivres/scoop-bucket
scoop install mdrev                     # Windows
go install github.com/esivres/mdrev/cmd/mdrev@latest
```

## Getting started

Install the mdrev extension from the Zed extensions panel, then configure the
editor once for the machine:

```sh
mdrev setup                             # asks which shortcut to bind
```

That is all the editor needs, in every project: the extension declares the
language server, so Zed starts it for Markdown on its own, and `setup` writes
the comment tasks into Zed's global `tasks.json`.

Per project there is only one optional step — teaching a coding agent how the
review works:

```sh
cd ~/documents/spec
mdrev init --agent-docs both            # AGENTS.md section and/or a Claude Code skill
```

In the editor: select text, press the shortcut, type a comment. It gets
underlined in the document; hovering shows the text, and the code action menu
offers "Apply suggestion" and "Dismiss / mark resolved".

The same from a terminal:

```sh
mdrev comment --file spec.md --quote "no more than 2" --type issue
mdrev list spec.md
mdrev reply --file spec.md --id 9b9e4214
```

## How it works in Zed

The mdrev extension exists for one reason: Zed only lets an *extension* declare
a language server, never a settings file. With the extension installed nothing
else is configured — Zed runs several servers per language, so mdrev's review
comments appear next to whatever else you use for Markdown.

Without the extension, `mdrev init` falls back to overriding the binary of the
markdownlint extension, which does declare a Markdown server. That works, but
costs you markdownlint and has to be repeated per project.

Comment input does not go through LSP: the protocol has no way to ask a human
for text. `setup` writes Zed tasks that use `ZED_SELECTED_TEXT` and binds them
to your shortcut.

## Compatibility

mdrev writes valid MRSF: what it produces is accepted by the reference
validator, `mrsf validate`. Suggested edits are stored in `x_suggested_text`,
inside the extension namespace the specification reserves.

Why the format was adopted but not its tooling is written up in
[ADR-0001](docs/adr-0001-storage-and-tooling.md).

## Limitations

- Replies and resolving someone else's comment are CLI-only; the editor offers
  resolve through a code action.
- A multi-line comment is typed until `Ctrl+D`, or composed with `--editor`.
- Reading the document in a browser is a second stage.

## License

MIT
