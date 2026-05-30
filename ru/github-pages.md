# GitHub Pages

## Purpose

The repository includes a static bilingual GitHub Pages landing page for the
project. The page is intentionally separate from the Stage 08 product frontend:
it is repository documentation and project presentation, not the runtime Web UI.

Both language variants use the same brand-led first viewport with dark
navigation, Terraform discovery/security artwork and direct documentation
entry points.

## Files

- `docs/index.html` - Russian landing page and default GitHub Pages entrypoint.
- `docs/en.html` - English landing page.
- generated `*.html` documentation pages in the artifact root - Russian
  documentation shell.
- generated `en/**/*.html` documentation pages - English documentation shell.
- `docs/**/*.md` outside `docs/en/` - Russian Markdown documentation sources.
- `docs/en/**/*.md` - corresponding English Markdown documentation sources
  with the same relative paths.
- `docs/pages.css` - shared responsive styles.
- `docs/pages.js` - shared reveal and hero interaction behavior.
- `docs/.nojekyll` - disables Jekyll processing for the Pages artifact.
- `.github/workflows/pages.yml` - GitHub Actions deployment workflow.

The landing language switcher uses plain static links:

- Russian to English: `en.html`;
- English to Russian: `index.html`.

Generated document pages also include a static RU/EN switcher for the same
document path. This keeps the published site usable even when JavaScript is
unavailable.

Markdown documents under `docs/ru/**/*.md` are the canonical Russian sources.
Matching Markdown documents under `docs/en/**/*.md` are the canonical English
sources for the same document paths. During publication,
`docs/build-pages.js` uses `docs/ru` as the path catalog, generates Russian
pages at the Pages root and English pages under `en/`, rewrites internal
Markdown links to the generated same-language HTML pages and links inline
document references such as `docs/ru/api.md` or `docs/en/api.md` when the
target exists. The published Pages site therefore shows documentation as
complete styled pages with common navigation, local table of contents, related
documents, backlinks and a full documentation catalog. The raw same-language
`.md` source remains available from each generated document page.

## Deployment

The `github-pages` workflow publishes the `docs` directory to the `gh-pages`
branch on pushes to `master` and on manual `workflow_dispatch` runs.

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

- Keep Russian and English Markdown sources aligned when updating
  documentation or page copy.
- Keep the brand-led hero, product mark and primary documentation entry points
  visible in the first viewport for both language variants.
- Keep links inside the Pages artifact relative to `docs` unless the target
  file is intentionally linked on GitHub.
- Keep generated document pages buildable with the repository-local Node.js
  script; do not require package installation for Pages publication.
- Keep cross-document references as relative Markdown links or inline
  `docs/ru/...md` / `docs/en/...md` references so the Pages generator can
  produce HTML links and backlinks.
- Do not treat the Pages landing page as the Stage 08 Web UI; runtime UI
  contracts remain in `docs/ru/frontend-ui-contract.md`.
