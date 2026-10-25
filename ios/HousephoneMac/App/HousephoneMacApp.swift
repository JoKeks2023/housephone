import SwiftUI

@main
struct HousephoneMacApp: App {
    @State private var bridge = MacBridge()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(bridge)
                .frame(minWidth: 380, minHeight: 520)
                .task { bridge.start() }
        }
        .defaultSize(width: 420, height: 640)
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { bridge.refresh() }
        }
    }
}
