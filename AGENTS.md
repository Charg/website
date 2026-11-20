# Agent notes

## Checks

- Run `prek run --all-files` before committing; CI runs the same hooks.
- Go tools (staticcheck, govulncheck) are pinned as `tool` deps in `server/go.mod`.

## GitHub Actions

- Pin every `uses:` to a full commit SHA with a version comment.
- After adding or bumping an action, regenerate the lockfile with
  `gh actions-lock --no-narrow`. Without `--no-narrow` it rewrites SHA pins to
  tags, which zizmor's `unpinned-uses` audit rejects.
