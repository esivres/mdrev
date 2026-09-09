Documents in this project are reviewed with `mdrev`. Review comments live in a
sidecar next to the document (`doc.md.review.yaml`), never inside the document.

**Never edit the sidecar by hand** — it is a specified format with anchor
hashes that the commands maintain.

## Reading what the human wrote

```sh
mdrev list doc.md            # open threads, human-readable
mdrev list doc.md --json     # the same for a program
```

Check this after you hand a document over, and again whenever you are asked
what the review says. An unanswered comment is work you still owe.

## Answering

```sh
mdrev reply --file doc.md --id 9b9e4214 --author Claude --text "..."
```

`--id` takes an id prefix. Without `--text` the reply is read from stdin, which
is how you send several paragraphs. Answer in the thread rather than in chat:
the thread is what the human sees next to the text.

Add `--resolve` only when the thread is genuinely finished — you made the
change, or you both agreed nothing is needed.

## Proposing a change to the text

```sh
mdrev comment --file doc.md --type suggestion \
  --quote "the exact fragment being replaced" \
  --suggest "the replacement text" \
  --author Claude --text "why this is better"
```

The human then applies or dismisses it from the editor in one action. Prefer
this over rewriting the document yourself: a suggestion is reviewable, a silent
edit is not.

## Commenting without proposing an edit

```sh
mdrev comment --file doc.md --quote "fragment" --type issue --severity high \
  --author Claude --text "..."
```

Use `--type issue` for something wrong, `--type question` when you need the
author to decide. Anchor on the shortest fragment that is unique in the
document — the anchor follows the text as the document is edited, and is
flagged as orphaned once the fragment is gone.
