Test fixture for scripts/__tests__/check-go-package-coverage.test.sh.

The file is stored as `sample.coverprofile.txt` (not `.coverprofile`)
because the repo root `.gitignore` matches `*.coverprofile` — the
`.txt` suffix keeps the fixture tracked while preserving Go's standard
coverprofile line format so the test can feed it to
`scripts/check-go-package-coverage.sh` unchanged.

The fixture uses a synthetic module prefix (kubestellar-mcp) and two
packages:

  - pkg/example      — 3 statements, all covered (100%)
  - pkg/example/sub  — 4 statements, 2 covered (50%)

Combined 5/7 = ~71.4%. This shape lets the test verify that the
script scopes each package's coverage to its own directory and does
NOT fold sub-package statements into the parent — the "scope
isolation" assertion in Case 5.
