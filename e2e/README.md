# Synthetic terminal tests

These suites run the real `g5a` binary in a pseudo-terminal against local, synthetic Grafana HTTP fixtures. They contain no credentials or company-specific dashboard data.

Run all suites on Linux:

```sh
make test-terminal
```

The Make target builds a fresh binary. Set `PYTHON` if needed, for example `make test-terminal PYTHON='uv run python'`.

The harness uses only the Python standard library, plus `bash`, util-linux `script`, and coreutils `timeout`. Each suite creates a fresh `/tmp/g5a-e2e-*` directory for terminal transcripts and captured requests. Set `G5A_E2E_WORK_DIR` to choose another artifact directory. Set `G5A_BINARY` when running a suite directly instead of through Make.

- `e2e_g5a.py`: merged tables, calculations, alert lists, and time-aligned series.
- `e2e_row_repeats.py`: repeats, variables, URL options, time handling, units, tables, and transformations.
- `e2e_v2_layouts.py`: schema-v2 layouts, library panels, tabs, geomaps, and vendor labels.
