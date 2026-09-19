# Релизы

[English](../releasing.md) · **Русский**

GitHub Releases, сообщения тегов и тело релиза — **на английском**. Берите [CHANGELOG.md](../../CHANGELOG.md), не русскую копию.

## Чеклист

1. Перенесите пункты `## Unreleased` в `CHANGELOG.md` под заголовок версии (`## 0.0.4 — YYYY-MM-DD`). То же зеркально в `CHANGELOG.ru.md`.
2. `go test ./...`
3. `make tar-ros` (и бинарь linux/amd64, если его кладёте в релиз).
4. Тег и push:

```bash
git tag v0.0.4
git push origin v0.0.4
```

5. Релиз GitHub **на английском**:

```bash
gh release create v0.0.4 \
  --title "v0.0.4" \
  --notes-file - <<'EOF'
See CHANGELOG.md for the full list.

- linux/arm64 binary and RouterOS tar (`mikrollm-ros-legacy.tar`)
- linux/amd64 binary (if attached)
EOF
```

Приложите `dist/mikrollm` (arm64), при наличии `dist/mikrollm-linux-amd64` и `dist/mikrollm-ros-legacy.tar`.

Пароли RouterOS и админки в notes не пишите.
