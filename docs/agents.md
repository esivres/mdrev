# Working with coding agents

The point of the CLI is that an agent reviews the same document you do, in the
same threads, without anyone pasting text into a chat window.

## Installing the instructions

```sh
mdrev init --agent-docs both
```

This writes a section into `AGENTS.md` and a Claude Code skill into
`.claude/skills/mdrev/SKILL.md`. Both carry the same content: how to read
comments, how to answer in a thread, how to propose an edit, and the rule not
to touch the sidecar by hand. `--agent-docs agents` or `skill` installs just
one. An existing `AGENTS.md` is appended to, and never duplicated.

## The loop

1. The agent writes a document and hands it over.
2. You read it in the editor and comment — `Alt+C` on a selection, or a
   `{>>marker<<}` typed into the text, or `n` in `mdrev threads`.
3. The agent runs `mdrev list doc.md --json`, sees what is open, and answers in
   the thread with `mdrev reply`.
4. Where the answer is a change to the text, the agent proposes it rather than
   making it:

```sh
mdrev comment --file doc.md --type suggestion \
  --quote "the exact fragment" --suggest "the replacement" \
  --author Claude --text "why this is better"
```

5. You apply or dismiss it from the editor in one action.

Step 4 is the part worth insisting on. A silent rewrite is unreviewable: you
have to diff the document to find out what the agent decided. A suggestion is
visible where the text is, and applying it is your action, not the agent's.

## Watching for changes

The language server polls the sidecar's modification time once a second, so a
comment the agent files from the CLI appears in your editor without you
reloading anything. The reverse direction needs no polling at all: the agent
reads the sidecar when it runs `mdrev list`.

## Authorship

Comments made from the editor are attributed to `git config user.name`, and an
agent should pass `--author` with its own name. This is what makes a thread
readable later — who asked, who answered.
