import SwiftUI

@main
struct HousephoneMacApp: App {
    var body: some Scene {
        WindowGroup {
            RootView()
                .frame(minWidth: 380, minHeight: 520)
        }
        .defaultSize(width: 420, height: 640)
    }
}
