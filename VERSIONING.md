# Semantic Versioning

This project follows [Semantic Versioning 2.0](https://semver.org/): **MAJOR.MINOR.PATCH**.

- **MAJOR**: Incompatible API or storage changes (e.g. breaking REST/MCP, DB schema).
- **MINOR**: New features, backward-compatible (e.g. new endpoints, new memory type).
- **PATCH**: Backward-compatible fixes (bugfixes, docs, no API change).

The current version is in the `VERSION` file. Tag releases with `v` + version, e.g. `v1.3.0`.

## Commands

- `make version` — print current version
- `make tag` — create git tag from `VERSION` (e.g. `v1.3.0`) and push tags (if remote exists)
- `make bump-patch` / `make bump-minor` / `make bump-major` — increment `VERSION` and show new value (commit separately)

## Workflow

1. Change code, run `make test`.
2. Update `VERSION` (or use `make bump-patch` etc.).
3. Commit with a message that references the version or change.
4. Run `make tag` to create and push the version tag.
