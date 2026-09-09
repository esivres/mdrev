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

```sh
cd ~/documents/spec
mdrev init                              # sets up the editor
```

`init` asks which shortcut to bind, or takes one with `--keys ctrl-alt-k`.
After that, in the editor: select text, press the shortcut, type a comment. It
gets underlined in the document; hovering shows the text, and the code action
menu offers "Apply suggestion" and "Dismiss / mark resolved".

The same from a terminal:

```sh
mdrev comment --file spec.md --quote "no more than 2" --type issue
mdrev list spec.md
mdrev reply --file spec.md --id 9b9e4214
```

## How it works in Zed

Zed cannot register a language server from settings — only an extension can
declare one. `mdrev init` works around this by taking over the Marksman
extension, which declares itself the server for Markdown, and overriding its
binary. Install Marksman from the extensions panel; `init` does the rest.

Comment input does not go through LSP: the protocol has no way to ask a human
for text. `init` writes Zed tasks that use `ZED_SELECTED_TEXT` and binds them
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
