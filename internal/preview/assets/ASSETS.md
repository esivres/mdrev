# Preview assets

Vendored, served from the binary via go:embed. No CDN at runtime.

| File | Package | Version | License | Source |
|---|---|---|---|---|
| lib/markdown-it.min.js | markdown-it | 14.1.0 | MIT | cdn.jsdelivr.net/npm/markdown-it@14.1.0/dist/markdown-it.min.js |
| lib/markdown-it-task-lists.min.js | markdown-it-task-lists | 2.1.1 | MIT | cdn.jsdelivr.net/npm/markdown-it-task-lists@2.1.1/dist/markdown-it-task-lists.min.js |
| lib/highlight.min.js | @highlightjs/cdn-assets | 11.11.1 | BSD-3 | cdn.jsdelivr.net/npm/@highlightjs/cdn-assets@11.11.1/highlight.min.js |
| lib/katex.js | katex | 0.15.3 | MIT | markdown-preview.nvim `_static/katex@0.15.3.js` |
| lib/katex-auto-render.min.js | katex | 0.15.3 | MIT | cdn.jsdelivr.net/npm/katex@0.15.3/dist/contrib/auto-render.min.js |
| lib/mermaid.min.js | mermaid | (UMD build shipped by markdown-preview.nvim) | MIT | markdown-preview.nvim `_static/mermaid.min.js` |
| css/katex.css | katex | 0.15.3 | MIT | markdown-preview.nvim `_static/katex@0.15.3.css` |
| css/markdown.css | markdown-preview.nvim | — | MIT | markdown-preview.nvim `_static/markdown.css` |
| css/highlight.css | highlight.js theme | — | BSD-3 | markdown-preview.nvim `_static/highlight.css` |
| fonts/*.woff2 | katex | 0.15.3 | OFL | markdown-preview.nvim `_static/fonts/` |

Refresh with pinned versions and verify checksums:

	sh scripts/vendor-preview-assets.sh
