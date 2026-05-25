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
- `docs/.nojekyll` - disables Jekyll processing for the Pages artifact.
- `.github/workflows/pages.yml` - GitHub Actions deployment workflow.

The language switcher uses plain static links:

- Russian to English: `en.html`;
- English to Russian: `index.html`.

This keeps the published site usable even when JavaScript is unavailable.

## Deployment

The `github-pages` workflow publishes the `docs` directory as the Pages
artifact on pushes to `master` and on manual `workflow_dispatch` runs.

Repository settings must use:

- Pages source: `GitHub Actions`.

The workflow permissions are intentionally limited to:

- `contents: read`;
- `pages: write`;
- `id-token: write`.

## Maintenance contract

- Keep Russian and English content aligned when updating page copy.
- Keep the AI-only implementation notice visible before the hero content in
  both language variants.
- Keep links inside the Pages artifact relative to `docs` unless the target
  file is intentionally linked on GitHub.
- Do not treat the Pages landing page as the Stage 08 Web UI; runtime UI
  contracts remain in `docs/frontend-ui-contract.md`.
