# Notes for coding agents

- Design specs, plans and mockups go in `.superpowers/specs/`,
  `.superpowers/plans/` and `.superpowers/mockups/`. That folder is
  untracked and never published. Never write them to `docs/superpowers/`
  (ignored, and a test fails if anything there is tracked).
- User-facing docs are `README.md`, `CONTRIBUTING.md`, `SECURITY.md` and
  `docs/*.md`; tests in `internal/cli` and `internal/core` keep them true to
  the code. Read `CONTRIBUTING.md` before changing code.
