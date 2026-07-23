# openfsd webjs tests

Node unit tests for pure first-party browser modules under
`internal/web/static/js/openfsd/`. Sources are **not** duplicated here — tests
import via relative paths into the go:embed tree.

## Requirements

- Node.js **≥ 20** (CI pins Node 20 LTS)
- Zero production npm dependencies for unit tests (`node:test` / `node:assert/strict`)

## Run

From repo root:

```bash
bash scripts/check-webjs.sh
```

Or from this directory:

```bash
npm test
# optional coverage (Node experimental):
npm run test:coverage
```

## Layout

```text
webjs/
  package.json
  airport-editor/*.test.js   # imports ../../internal/web/static/js/openfsd/airport-editor/*
```

Canonical sources: `internal/web/static/js/openfsd/airport-editor/`.
Goldens / fixtures: `pkg/twrfiles/testdata/`.
