import SwiftUI

@main
struct HousephoneMacApp: App {
    @State private var bridge: MacBridge
    @State private var calls: MacCallCenter
    @Environment(\.scenePhase) private var scenePhase

    init() {
        let bridge = MacBridge()
        _bridge = State(initialValue: bridge)
        _calls = State(initialValue: MacCallCenter(bridge: bridge))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(bridge)
                .environment(calls)
                .frame(minWidth: 380, minHeight: 520)
                .task { bridge.start() }
        }
        .defaultSize(width: 420, height: 640)
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { bridge.refresh() }
        }
    }
}
