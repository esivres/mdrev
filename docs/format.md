# Storage format

Review data lives beside the document, never inside it:

```
doc.md                   the document
doc.md.review.yaml       the review
doc.md.review.yaml.lock  an empty file used to serialise writes
```

The lock file is created next to the review and never removed — deleting it is
the race where one process unlinks a file another has just opened. It holds no
data and can be ignored by version control.

The format is [MRSF](https://sidemark.org), the Markdown Review Sidecar Format.
mdrev reads and writes it directly, and its output is accepted by the reference
validator, `mrsf validate`. Why the format was adopted but not its tooling is
in [ADR-0001](adr-0001-storage-and-tooling.md).

## A sidecar

```yaml
mrsf_version: "1.0"
document: spec.md
comments:
  - id: 53c5f1e2-bbab-4c1a-b8d5-3c4915398fa4
    author: Claude
    timestamp: 2026-09-09T03:39:35.814Z
    text: The threshold is not justified.
    resolved: false
    line: 38
    type: issue
    severity: high
    selected_text: no more than 2
    selected_text_hash: 595166fb…
    x_suggested_text: no more than 1
```

`selected_text_hash` is the SHA-256 of `selected_text`.

## Anchors

A comment is found by its quoted text, not by its line: the line is only a hint
that disambiguates a fragment occurring several times. When the document is
edited, the comment follows the text; when the fragment disappears, the comment
survives, marked as orphaned rather than deleted. A lost comment is worse than
a misplaced one.

Every write rewrites the positions of every comment from the document as it is
at that moment: `line`, `end_line`, `start_column`, `end_column` — columns in
characters, not bytes — plus `x_reanchor_status` and `x_reanchor_score`, the
keys the reference tool uses. Without this the recorded position describes
where the text used to be, and decays with each edit above it. `mrsf reanchor`
finds nothing left to change after mdrev has written.

When the quoted text is not found exactly, the closest run of words in the
document is taken instead, provided it still resembles the quote — editing the
very sentence a comment is about is the ordinary case in review, and the remark
is usually why it changed. `x_reanchor_score` records how close the match was:
`1` for an exact one, less for a rewrite.

A comment whose fragment is gone beyond recognition is marked
`x_reanchor_status: orphaned` and keeps its last known line. `mdrev list` shows it as `[anchor lost]`, and
`--json` carries `"orphaned": true`, so an agent knows it is answering a thread
about a passage that no longer exists.

## Threads

A reply is a comment with `reply_to` pointing at its parent, and it inherits
the parent's anchor. Tools that do not understand threading still see valid
comments.

Replies may point at other replies; mdrev walks each one up to the comment that
starts the thread and shows them all there. A reply whose parent is missing —
deleted by another tool, or a broken hand edit — is shown as a thread of its
own rather than dropped.

## Suggested edits

MRSF has no field for a proposed replacement, so mdrev stores one in
`x_suggested_text`, inside the `x_*` extension namespace the specification
reserves. Other MRSF tools ignore it instead of failing. If the specification
ever describes suggested edits itself, mdrev will move to its fields.

## Drafts in the document

A comment can be typed straight into the document as CriticMarkup:

```markdown
Latency p99 must not exceed 200 ms. {>>too optimistic<<}
```

This is not storage — it is input. The marker is highlighted as a comment not
yet in the review, and its code action moves it into the sidecar, anchored to
the words in front of it, and out of the document. Nothing stays behind.

## Concurrent writes

Every write — from the CLI, the language server or the browser — goes through
one function that takes an exclusive lock on the lock file first. Without it a
review being written from two processes loses most of what is written: in a
test of 80 concurrent writes, 67 comments disappeared with no error anywhere.

The lock is advisory, so it binds mdrev and nothing else. A tool that knows
nothing about it — the reference `mrsf` CLI, an editor saving the YAML — can
still overwrite a concurrent change, and advisory locks are unreliable on
network filesystems and in file-syncing folders.

## Editing by hand

Don't. The anchor hash and the `reply_to` links are maintained by the commands,
and the file is rewritten in full whenever a thread is resolved or a comment is
added — so a hand-edited sidecar with a broken hash or a dangling reply keeps
the damage. Use `mdrev comment`, `mdrev reply` and `mdrev threads`.

Keys this tool does not model are preserved: anything else at the top level, or
inside a comment, survives a load and save. Their values are kept, not their
formatting — the file is re-emitted, so ordering, anchors and merge keys are
normalised. An unknown key that collides with a modelled field (`document`,
`resolved`, …) is dropped, because the encoder cannot represent both.
