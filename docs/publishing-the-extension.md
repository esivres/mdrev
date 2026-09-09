# Publishing the Zed extension

The extension lives in [`zed-extension/`](../zed-extension) and is submitted to
[zed-industries/extensions](https://github.com/zed-industries/extensions).
Its registry id is `mdrev-language-server`: the guidelines require an extension
that provides only a language server to say so in its id.

## Before submitting

- The extension must work from a published release. It resolves the binary from
  LSP settings, then `PATH`, then the latest GitHub release — a user with none
  of the first two gets nothing until a release with platform archives exists.
- Test it in Zed at exactly the commit being submitted, installed through
  `zed: install dev extension`.
- `version` in `zed-extension/extension.toml` must match the version recorded
  in the registry's `extensions.toml`.
- Only sources belong in the directory: `extension.wasm` and `target/` are
  build output and are ignored by git.
- `zed-extension/LICENSE` must stay: for an extension in a subdirectory, the
  licence at the repository root is not enough.

## Submitting

```sh
git clone https://github.com/<you>/extensions
cd extensions
git submodule init && git submodule update
git submodule add https://github.com/esivres/mdrev.git extensions/mdrev-language-server
git add extensions/mdrev-language-server
```

Then add an entry at the top of `extensions.toml`, pointing at the
subdirectory, and sort the file:

```toml
[mdrev-language-server]
submodule = "extensions/mdrev-language-server"
path = "zed-extension"
version = "0.1.0"
```

```sh
pnpm sort-extensions
```

The submodule URL must be HTTPS, the repository public, and the referenced
commit must exist on a branch rather than a detached HEAD.

## House rules of the registry

One extension per pull request, at most three open at a time, and maintainer
feedback answered within three weeks or the PR is closed. Submission is
reviewed, not automatic.

## Updating a published version

Bump `version` in `zed-extension/extension.toml`, push, then open a second PR
moving the submodule to the new commit and updating the version in
`extensions.toml` to match.
