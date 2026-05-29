# GitHub Pages

## Purpose

The repository includes a static bilingual GitHub Pages landing page for the
project. The page is intentionally separate from the Stage 08 product frontend:
it is repository documentation and project presentation, not the runtime Web UI.

Both language variants start with a visible notice that the project is
implemented exclusively by AI.

## Files

- `docs/index.html` - Russian landing page and default GitHub Pages entrypoint.
- `docs/en.html` - English landing page.
- `docs/pages.css` - shared responsive styles.
- `docs/pages.js` - shared reveal and hero interaction behavior.
- `docs/build-pages.js` - static Markdown-to-HTML generator for published
  documentation pages.
- `docs/.nojekyll` - disables Jekyll processing for the Pages artifact.
- `.github/workflows/pages.yml` - GitHub Actions deployment workflow.

The language switcher uses plain static links:

- Russian to English: `en.html`;
- English to Russian: `index.html`.

This keeps the published site usable even when JavaScript is unavailable.

Markdown documents under `docs/**/*.md` are the canonical sources. During
publication, `docs/build-pages.js` generates sibling `*.html` pages for every
Markdown file and rewrites internal Markdown links to the generated HTML pages.
The published Pages site therefore shows documentation as complete styled
pages with page navigation, while the raw `.md` sources remain available from
each generated document page.

## Deployment

The `github-pages` workflow publishes the `docs` directory to the `gh-pages`
branch on pushes to `master` and on manual `workflow_dispatch` runs.

Before copying files to the publish worktree, the workflow runs:

```text
node docs/build-pages.js --out "$publish_source"
```

The generator copies the static assets, renders all Markdown files to HTML and
keeps the generated pages dependency-free.

Repository settings must use:

- Pages source: `Deploy from a branch`;
- branch: `gh-pages`;
- folder: `/ (root)`.

This branch-based deployment avoids the GitHub Pages REST API calls used by
`actions/configure-pages` and `actions/deploy-pages`. Those REST calls can fail
with `Resource not accessible by integration` when the repository or
organization does not allow `GITHUB_TOKEN` to create or configure the Pages
site.

The workflow permissions are intentionally limited to:

- `contents: write`.

## Maintenance contract

- Keep Russian and English content aligned when updating page copy.
- Keep the AI-only implementation notice visible before the hero content in
  both language variants.
- Keep links inside the Pages artifact relative to `docs` unless the target
  file is intentionally linked on GitHub.
- Keep generated document pages buildable with the repository-local Node.js
  script; do not require package installation for Pages publication.
- Do not treat the Pages landing page as the Stage 08 Web UI; runtime UI
  contracts remain in `docs/frontend-ui-contract.md`.
