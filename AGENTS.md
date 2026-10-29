# Agent notes

- Build: `make build` (binary in `bin/hapiq`).
- Unit tests: `make test`. Lint: `make lint`.
- Curated end-to-end suite: `make curated`, or `make curated RUN=geo-001` for
  one case. Real downloads (about 400 MB), not run in CI; run it after
  changing download, filter or witness code. Each bug repro gets a case; see
  `test/curated/README.md`.
- Never commit `BUGS.md`; it is a local file.
