# Releases

**English** · [Русский](ru/releasing.md)

GitHub Releases, tag messages, and the release body are **English**. Use [CHANGELOG.md](../CHANGELOG.md), not the Russian copy.

## Checklist

1. Move `## Unreleased` items in `CHANGELOG.md` under a version heading (`## 0.0.4 — YYYY-MM-DD`). Mirror the same move in `CHANGELOG.ru.md`.
2. `go test ./...`
3. `make tar-ros` (and linux/amd64 binary if you ship it).
4. Tag and push:

```bash
git tag v0.0.4
git push origin v0.0.4
```

5. Create the GitHub release **in English**:

```bash
gh release create v0.0.4 \
  --title "v0.0.4" \
  --notes-file - <<'EOF'
See CHANGELOG.md for the full list.

- linux/arm64 binary and RouterOS tar (`mikrollm-ros-legacy.tar`)
- linux/amd64 binary (if attached)
EOF
```

Attach `dist/mikrollm` (arm64), `dist/mikrollm-linux-amd64` if built, and `dist/mikrollm-ros-legacy.tar`. The arm64 binary is also the Keenetic Entware asset (`make build-keenetic`).

Do not paste RouterOS or admin passwords into release notes.
