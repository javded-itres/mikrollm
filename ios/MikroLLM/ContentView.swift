import SwiftUI
import WebKit

struct ContentView: View {
    @EnvironmentObject var engine: Engine

    var body: some View {
        ZStack {
            if let url = engine.readyURL {
                AdminWebView(url: url)
                    .ignoresSafeArea()
            } else if !engine.error.isEmpty {
                VStack(spacing: 12) {
                    Text("MikroLLM").font(.title.bold())
                    Text(engine.error).foregroundStyle(.red).multilineTextAlignment(.center)
                }
                .padding()
            } else {
                VStack(spacing: 12) {
                    ProgressView()
                    Text("Запуск MikroLLM…")
                }
            }
        }
        .alert("Пароль админки", isPresented: Binding(
            get: { engine.showPassword && !engine.password.isEmpty && engine.readyURL != nil },
            set: { engine.showPassword = $0 }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(engine.password)
        }
    }
}

struct AdminWebView: UIViewRepresentable {
    let url: URL

    func makeUIView(context: Context) -> WKWebView {
        let cfg = WKWebViewConfiguration()
        let view = WKWebView(frame: .zero, configuration: cfg)
        view.scrollView.keyboardDismissMode = .interactive
        if #available(iOS 16.4, *) {
            view.isInspectable = true
        }
        view.load(URLRequest(url: url))
        return view
    }

    func updateUIView(_ uiView: WKWebView, context: Context) {}
}
