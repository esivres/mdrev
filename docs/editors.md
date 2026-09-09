# Editors

mdrev is a language server, so any editor with an LSP client can show review
comments. Zed gets an extension; elsewhere you point your client at
`mdrev lsp` for Markdown.

## What the server provides

- **Diagnostics** for every open comment, anchored to the quoted fragment.
  Replies are folded into the parent's message; a comment whose fragment is
  gone is kept and marked `[anchor lost]` rather than dropped.
- **Code actions**: `Apply suggestion` replaces the quoted text with the
  proposed one and closes the thread; `Dismiss / mark resolved` only closes it;
  `File as review comment` moves a CriticMarkup draft into the sidecar.
- **Live reload**: the sidecar's modification time is polled once a second, so
  a comment an agent adds from the CLI appears without touching the document.

The server never edits the document on its own — Zed implements neither
`workspace/applyEdit` nor `window/showDocument`, and every change therefore
travels as a code action the human accepts.

## Zed

Install the mdrev extension, then run `mdrev setup` once. That is all: an
extension declares the language server, and Zed starts it for Markdown without
any settings file. Zed runs several servers per language, so marksman or
markdownlint keep working next to mdrev.

`setup` writes two things:

- tasks in `~/.config/zed/tasks.json` — global, so no project repeats them;
- shortcuts in `~/.config/zed/keymap.json`, emitted for two contexts:

```json
{ "context": "Editor && !VimControl", "bindings": { … } }
{ "context": "VimControl && !menu",   "bindings": { … } }
```

The second is required with `vim_mode` on: a binding under `Editor` alone never
fires in normal or visual mode, because the vim layer takes the key first.

The thread browser opens in the dock below the document. A task can only reveal
in the dock or the centre area — there is no split target — and the dock keeps
the document in view, which is what the browser is for.

### Without the extension

`mdrev init` falls back to overriding the binary of the markdownlint
extension, which does declare a Markdown server:

```json
"lsp": { "markdownlint": { "binary": {
  "path": "/path/to/mdrev", "arguments": ["lsp"], "ignore_system_version": true } } }
```

This works, but costs you markdownlint and has to be repeated in every project.

## Other editors

Register `mdrev lsp` as a Markdown language server. Everything except the Zed
tasks works unchanged, and comments can still be written as CriticMarkup
markers and filed with a code action, which needs no editor-specific setup at
all.

Neovim 0.11 or newer, using its built-in LSP configuration:

```lua
vim.lsp.config.mdrev = {
  cmd = { "mdrev", "lsp" },
  filetypes = { "markdown" },
  root_markers = { ".git" },
}
vim.lsp.enable("mdrev")
```

VS Code has no generic "run this binary as a language server" setting; it needs
a small extension wrapper, which does not exist yet.
