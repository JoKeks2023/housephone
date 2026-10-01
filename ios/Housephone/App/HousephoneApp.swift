import Intents
import SwiftUI

@main
struct HousephoneApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @Environment(\.scenePhase) private var scenePhase

    private let services = AppServices.shared

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(services.bridge)
                .environment(services.direct)
                .environment(services.callCenter)
                .environment(services.watchLink)
                .environment(services.contacts)
                .environment(services.fritzBox)
                .environment(services.appModel)
                .environment(services.favorites)
                .modelContainer(services.modelContainer)
                .onOpenURL { url in
                    services.appModel.open(url)
                }
                .onContinueUserActivity(NSStringFromClass(INStartCallIntent.self)) { activity in
                    startCall(from: activity)
                }
        }
        .onChange(of: scenePhase) { _, phase in
            guard phase == .active else { return }
            services.bridge.refresh()
            services.direct.refresh()
            services.contacts.reload()
            Task { await services.fritzBox.refreshAfterConnect() }
        }
    }

    /// Calls started from the iPhone's recents or contacts arrive as an
    /// `INStartCallIntent`.
    private func startCall(from activity: NSUserActivity) {
        guard let intent = activity.interaction?.intent as? INStartCallIntent,
              let person = intent.contacts?.first,
              let number = person.personHandle?.value
        else { return }
        let name = person.displayName.isEmpty ? nil : person.displayName
        Task { await services.callCenter.startCall(to: number, name: name) }
    }
}
