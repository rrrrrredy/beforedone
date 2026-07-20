# Deterministic stale-receipt demo

Copy this directory to a temporary directory, initialize and commit a Git
repository, then run `beforedone check unit`. Change `a + b` in `calculator.go`
to `a - b`: the old PASS becomes stale and the Stop hook blocks. Running the
check now produces FAIL. Restore `a + b` and run the check once more to produce
a fresh PASS.
