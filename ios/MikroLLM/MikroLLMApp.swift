import SwiftUI
import Mobile

@main
struct MikroLLMApp: App {
    @StateObject private var engine = Engine()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(engine)
                .onAppear { engine.start() }
        }
    }
}

final class Engine: ObservableObject {
    @Published var readyURL: URL?
    @Published var password: String = ""
    @Published var error: String = ""
    @Published var showPassword = false

    func start() {
        if readyURL != nil { return }
        do {
            let dir = try FileManager.default.url(
                for: .applicationSupportDirectory,
                in: .userDomainMask,
                appropriateFor: nil,
                create: true
            ).appendingPathComponent("mikrollm", isDirectory: true)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let stored = UserDefaults.standard.string(forKey: "adminPassword") ?? ""
            let pass = try startMobile(dir.path, "127.0.0.1:4000", stored)
            password = pass
            UserDefaults.standard.set(pass, forKey: "adminPassword")
            showPassword = stored.isEmpty
            waitHealth()
        } catch {
            self.error = error.localizedDescription
        }
    }

    deinit {
        var err: NSError?
        _ = MobileStop(&err)
    }

    private func waitHealth() {
        DispatchQueue.global(qos: .userInitiated).async {
            let health = Foundation.URL(string: "http://127.0.0.1:4000/health")!
            for _ in 0..<80 {
                let err = MobileLastError()
                if !err.isEmpty {
                    DispatchQueue.main.async { self.error = err }
                    return
                }
                if let data = try? Data(contentsOf: health),
                   String(data: data, encoding: .utf8)?.contains("true") == true {
                    DispatchQueue.main.async {
                        self.readyURL = Foundation.URL(string: "http://127.0.0.1:4000/admin")
                    }
                    return
                }
                Thread.sleep(forTimeInterval: 0.1)
            }
            DispatchQueue.main.async {
                self.error = self.error.isEmpty ? "server did not become ready" : self.error
            }
        }
    }
}

/// gobind exports C functions with an explicit NSError**, not Swift `throws`.
private func startMobile(_ dataDir: String, _ listen: String, _ password: String) throws -> String {
    var err: NSError?
    let pass = MobileStart(dataDir, listen, password, &err)
    if let err {
        throw err
    }
    return pass
}
