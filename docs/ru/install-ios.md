# iOS (первый шаг)

[English](../install-ios.md) · **Русский**

Первый шаг на iOS — **не** локальная модель. Это MikroLLM как HTTP-сервер внутри приложения и существующая админка в **WKWebView**. GGUF / llama.cpp — следующий этап, бэкенд `llamacpp`.

```
WKWebView  →  http://127.0.0.1:4000/admin
                  ↑
            Go MikroLLM (gomobile bind)
                  ↑
            SQLite в Application Support
```

Нужен Mac с **Xcode** (один раз `xcodebuild -runFirstLaunch`), **Go 1.23+** и [XcodeGen](https://github.com/yonaskolb/XcodeGen) (`brew install xcodegen`). В этом шаге только Apple Silicon (`ios/arm64` + `iossimulator/arm64`).

## Сборка

```bash
make ios
open ios/MikroLLM.xcodeproj
```

`make ios` вызывает `gomobile bind` → `ios/Mobile.xcframework` и генерирует проект Xcode. В Xcode выберите симулятор или устройство и Run.

Если Xcode пишет **iOS 18.x is not installed**, откройте **Xcode → Settings → Platforms** и скачайте iOS (или `xcodebuild -downloadPlatform iOS`). Одной папки SDK недостаточно.

При первом запуске диалог покажет **пароль админки**. В WebView: Статус, добавьте OpenRouter / hub / Ollama в LAN. На телефон ничего не сидится.

## Состав

| Путь | Роль |
|---|---|
| `mobile` | `Start` / `Stop` / `URL` для gomobile (только `string`/`error`; не в `internal/`) |
| `ios/MikroLLM/*.swift` | оболочка SwiftUI + WKWebView |
| `ios/project.yml` | спецификация XcodeGen |
| `ios/Mobile.xcframework` | результат bind (не в git) |

Слушаем `127.0.0.1:4000`. Включён `NSAllowsLocalNetworking`. Seed `mobile` (без LAN `mac-80`).

## Ограничения этого шага

- Нет фона: iOS заморозит процесс, когда уйдёте из приложения.
- Нет подписи / CI для App Store.
- Нет llama.cpp. Следующий шаг iOS: встроить llama-server или HTTP-бэкенд `llamacpp` для 1–4B GGUF.

## Если не собирается

- **gomobile bind** падает на SQLite: тот же Go, что `go test ./mobile`.
- **gomobile: missing golang.org/x/mobile**: не удаляйте `tools.go`; не ставьте `golang.org/x/mobile@latest` (это поднимет Go 1.26). `make ios` пинит `MOBILE_VER`.
- Первый `xcodebuild -create-xcframework` после установки Xcode: `xcodebuild -runFirstLaunch`.
- Health не поднимается в симуляторе: ошибка на экране из `MobileLastError()`.
- Пустой WebView / ATS: в Info.plist должен быть `NSAllowsLocalNetworking`.
