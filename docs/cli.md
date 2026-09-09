# Command reference

Every command works on a document and its sidecar. Nothing here modifies the
document itself, except applying a suggested edit from the editor.

## mdrev setup

Configures Zed once for the machine: writes the review tasks into
`~/.config/zed/tasks.json` and offers shortcuts for them.

| flag | meaning |
|---|---|
| `--keys alt-c` | shortcut for the comment task; a second one may follow after a comma, otherwise the shift variant is derived |
| `--write-keymap` | write the shortcuts into `~/.config/zed/keymap.json` instead of printing them |
| `--no-keymap` | skip shortcuts entirely |

Without `--keys`, and with a terminal attached, it asks. The keymap is emitted
for two contexts, plain and vim — see [editors.md](editors.md).

## mdrev init

Prepares one project. With the extension installed there is nothing to
configure for the editor, so this only installs instructions for coding agents.

| flag | meaning |
|---|---|
| `--agent-docs agents\|skill\|both\|none` | write an `AGENTS.md` section, a Claude Code skill in `.claude/skills/mdrev`, both, or nothing |

If the mdrev extension is missing, `init` also writes `.zed/settings.json` that
borrows the markdownlint server slot, as a fallback.

## mdrev comment

Adds a comment. The text comes from stdin (finish with `Ctrl+D`) unless given.

| flag | meaning |
|---|---|
| `--file` | the document; required |
| `--quote` | the fragment to anchor to |
| `--line` | 1-based line, used to disambiguate a repeated quote |
| `--text` | comment text; read from stdin when omitted |
| `--editor` | compose in `$EDITOR` instead of stdin |
| `--type` | `issue`, `suggestion` or `question` |
| `--severity` | `low`, `medium` or `high` |
| `--suggest` | replacement text, applied from the editor in one action |
| `--author` | defaults to `git config user.name`, then `$USER` |

```sh
mdrev comment --file spec.md --type suggestion \
  --quote "no more than 2" --suggest "no more than 1" \
  --text "the threshold is not justified"
```

## mdrev reply

Replies in a thread. A reply inherits its parent's anchor.

| flag | meaning |
|---|---|
| `--file` | the document; required |
| `--id` | the comment to reply to; an id prefix is enough |
| `--text` | reply text; read from stdin when omitted |
| `--editor` | compose in `$EDITOR` |
| `--resolve` | mark the thread resolved after replying |
| `--author` | as for `comment` |

## mdrev apply

Applies a comment's suggested edit to the document and closes the thread as
`applied`. This is the only command that writes to the document itself.

| flag | meaning |
|---|---|
| `--file` | the document; required |
| `--id` | the comment whose suggestion to apply; an id prefix is enough |

The replacement lands on the occurrence nearest the comment, not the first one
in the file.

## mdrev resolve

Closes a thread without replying to it.

| flag | meaning |
|---|---|
| `--file` | the document; required |
| `--id` | the comment to close; an id prefix is enough |
| `--dismiss` | record it as turned down rather than settled |
| `--reopen` | reopen a closed thread instead |

## mdrev list

Prints open threads with their replies. `--json` gives an agent the same
threads with each one's `replies` nested inside, using the field names of the
sidecar (`id`, `selected_text`, `x_suggested_text`), and prints `[]` when the
review is clean.

```sh
mdrev list spec.md
mdrev list spec.md --json
mdrev list spec.md --json --all   # resolved threads too, with x_outcome
```

A thread whose quoted text is gone is printed with `[anchor lost]`, and carries
`"orphaned": true` in JSON.

Closing a thread records how it ended — `applied`, `dismissed` or `resolved` —
in `x_outcome`. Without that, an accepted suggestion and a rejected one leave
identical state, and an agent proposes the same edit again.

## mdrev threads

Opens the terminal UI over one document.

| key | action |
|---|---|
| `j` / `k` | move between threads |
| `ctrl+d` / `ctrl+u` | scroll the thread by half a page (`f`/`b` for a full one) |
| `n` | start a new thread on the line you came from |
| `r` | reply to the selected thread (`Ctrl+S` sends, `Esc` cancels) |
| `x` | resolve or reopen |
| `a` | show resolved threads too, and hide them again |
| `o` | open the document at the thread's line in `$EDITOR` |
| `q` | quit |

`--line N` selects the thread nearest that line, and anchors a new comment
there. The Zed task passes `$ZED_ROW` for this. Nearness is measured against
where each anchor actually is in the document now, not the line recorded in the
sidecar, which drifts as the document is edited.

The selected thread is shown with the paragraph it is about, with the anchored
fragment highlighted inside it.

## mdrev lsp

Runs the language server on stdio. Editors start it; you never run it by hand,
and it takes no flags.
It traces to `$TMPDIR/mdrev-lsp.log`, which is the first place to look when the
editor shows nothing.
