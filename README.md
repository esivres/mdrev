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
# macOS and Linux
brew tap esivres/mdrev https://github.com/esivres/mdrev
brew install esivres/mdrev/mdrev

# Windows
scoop bucket add mdrev https://github.com/esivres/mdrev
scoop install mdrev

# from source
go install github.com/esivres/mdrev/cmd/mdrev@latest
```

The tap and the bucket are this repository — `brew tap` and `scoop bucket add`
take an explicit URL, so there is no separate `homebrew-*` repository to add.

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

In the editor, type a comment straight into the text where you want it, using
CriticMarkup:

```markdown
Latency p99 must not exceed 200 ms per request. {>>too optimistic<<}
```

The marker is highlighted as a comment not yet in the review; the code action on it
(`ctrl-.` in Zed's default keymap, `Alt+Enter` with the JetBrains one) moves it
into the sidecar and out of the document, anchored to the words in
front of it. This needs no shortcuts and no
configuration, and works in any editor with an LSP client.

Filed comments are underlined in the document; hovering shows the thread, and
the code action menu offers "Apply suggestion" and "Keep current wording"
on a proposed edit, or "Resolve comment" on a plain remark.

If you would rather select text and press a key, `mdrev setup` also installs
Zed tasks bound to a shortcut of your choice.

Inline diagnostics show one line, which is not enough for a discussion, so
whole threads live in a terminal UI:

```sh
mdrev threads spec.md
```

`j`/`k` move between threads, `n` starts a new one, `r` replies, `x` resolves,
`a` shows resolved ones too, `o` opens the document at the thread's line.

Each thread is shown with the paragraph it is about, the anchored fragment
highlighted inside it.

`mdrev setup` binds this to `Alt+T` next to `Alt+C`, passing the line you are
on: the thread about that spot opens first, and a new comment anchors there.
The browser opens in the dock, so the document stays in view.

The same from a terminal:

```sh
mdrev comment --file spec.md --quote "no more than 2" --type issue
mdrev list spec.md
mdrev reply --file spec.md --id 9b9e4214
mdrev threads spec.md
```

## How it works in Zed

The mdrev extension exists for one reason: Zed only lets an *extension* declare
a language server, never a settings file. With the extension installed nothing
else is configured — Zed runs several servers per language, so mdrev's review
comments appear next to whatever else you use for Markdown.

Without the extension, `mdrev init` falls back to overriding the binary of the
markdownlint extension, which does declare a Markdown server. That works, but
costs you markdownlint and has to be repeated per project.

Comment input cannot go through LSP directly: the protocol has no way to ask a
human for text, and Zed supports neither `window/showDocument` nor
`workspace/applyEdit`, so a server cannot open a scratch buffer or edit the
document on its own. Hence CriticMarkup markers — the document is the one input
surface a language server can offer. The Zed tasks `setup` installs are the
alternative for people who prefer selecting text and pressing a key.

## Compatibility

mdrev writes valid MRSF: what it produces is accepted by the reference
validator, `mrsf validate`. Suggested edits are stored in `x_suggested_text`,
inside the extension namespace the specification reserves.

Why the format was adopted but not its tooling is written up in
[ADR-0001](docs/adr-0001-storage-and-tooling.md).

## Documentation

- [Command reference](docs/cli.md)
- [Editors](docs/editors.md) — Zed, and any other LSP client
- [Storage format](docs/format.md)
- [Working with coding agents](docs/agents.md)

## Limitations

- Replies and resolving someone else's comment are CLI-only; the editor offers
  resolve through a code action.
- A multi-line comment is typed until `Ctrl+D`, or composed with `--editor`.
- Reading the document in a browser is a second stage.

## License

MIT
