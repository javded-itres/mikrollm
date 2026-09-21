# iOS (first slice)

**English** · [Русский](ru/install-ios.md)

The first iOS step is **not** an on-device LLM. It is MikroLLM as a local HTTP server inside the app, with the existing admin UI in a **WKWebView**. Local GGUF / llama.cpp comes later as a `llamacpp` backend.

```
WKWebView  →  http://127.0.0.1:4000/admin
                  ↑
            Go MikroLLM (gomobile bind)
                  ↑
            SQLite in Application Support
```

Needs a Mac with **Xcode** (run `xcodebuild -runFirstLaunch` once), **Go 1.23+**, and [XcodeGen](https://github.com/yonaskolb/XcodeGen) (`brew install xcodegen`). Apple Silicon only in this slice (`ios/arm64` + `iossimulator/arm64`).

## Build

```bash
make ios
open ios/MikroLLM.xcodeproj
```

`make ios` runs `gomobile bind` → `ios/Mobile.xcframework` and generates the Xcode project. In Xcode pick an iPhone simulator or device, then Run.

If Xcode says **iOS 18.x is not installed**, open **Xcode → Settings → Platforms** and download iOS (or `xcodebuild -downloadPlatform iOS`). The SDK folder alone is not enough.

On first launch a dialog shows the **admin password**. Open Status in the WebView, add OpenRouter / hub / a LAN Ollama. Nothing is seeded onto the phone.

## Layout

| Path | Role |
|---|---|
| `mobile` | `Start` / `Stop` / `URL` for gomobile (`string`/`error` only; not under `internal/`) |
| `ios/MikroLLM/*.swift` | SwiftUI shell + WKWebView |
| `ios/project.yml` | XcodeGen spec |
| `ios/Mobile.xcframework` | bind output (gitignored) |

Listen address is `127.0.0.1:4000`. `NSAllowsLocalNetworking` is on. Seed is `mobile` (no LAN `mac-80` hosts).

## Limits (this slice)

- No background run: iOS suspends the process when you leave the app.
- No App Store build settings / signing in CI yet.
- No llama.cpp. Next iOS slice: embed llama-server or a `llamacpp` HTTP backend for 1–4B GGUF.

## Troubleshooting

- **gomobile bind** fails on SQLite: use the same Go as `go test ./mobile`.
- **gomobile: missing golang.org/x/mobile**: `tools.go` must stay in the module; do not `go get golang.org/x/mobile@latest` (that bumps Go 1.26). `make ios` pins `MOBILE_VER`.
- First `xcodebuild -create-xcframework` after installing Xcode: `xcodebuild -runFirstLaunch`.
- Simulator health never becomes ready: the error screen shows `MobileLastError()`.
- ATS / blank WebView: confirm Info.plist has `NSAllowsLocalNetworking`.
