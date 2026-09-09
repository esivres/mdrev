# mdrev language server for Zed

Shows review comments on Markdown documents inside the editor: each open
comment is a diagnostic anchored to the text it is about, and code actions
apply a suggested edit, resolve a thread, or file a comment typed into the
document as CriticMarkup.

Comments are stored in a sidecar file next to the document, in the
[MRSF](https://sidemark.org) format, so the Markdown itself is never modified.

The extension runs the `mdrev` binary. It uses one already on your PATH — from
`brew install esivres/mdrev/mdrev`, scoop, or `go install` — and otherwise
downloads the matching release from
[github.com/esivres/mdrev](https://github.com/esivres/mdrev).

Zed runs several language servers per language, so this works alongside
marksman or markdownlint rather than replacing them.
