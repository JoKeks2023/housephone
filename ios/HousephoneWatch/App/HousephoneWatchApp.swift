import HousephoneKit
import SwiftUI
import WatchKit

@main
struct HousephoneWatchApp: App {
    @WKApplicationDelegateAdaptor(WatchAppDelegate.self) private var appDelegate
    private let services = WatchServices.shared

    var body: some Scene {
        WindowGroup {
            WatchRootView()
                .environment(services.bridge)
                .environment(services.callCenter)
                .environment(services.recents)
                .environment(services.fritzBox)
                .environment(services.favorites)
                .onOpenURL { url in
                    services.open(url)
                }
        }
    }
}

/// Long-lived objects of the watch app. Created at launch — also when
/// watchOS launches the app for a VoIP push — so PushKit and CallKit are
/// ready before the push is delivered.
@MainActor
final class WatchServices {
    static let shared = WatchServices()

    let bridge: WatchBridge
    let recents: RecentCalls
    let callCenter: WatchCallCenter
    let phoneLink: PhoneLink
    let fritzBox: WatchFritzBox
    let favorites: WatchFavorites
    let snapshot: WatchSnapshotPublisher
    /// Proves that a call link comes from a complication (`DeepLinkKey`).
    let linkKey: String?

    private init() {
        bridge = WatchBridge(store: KeychainCredentialStore())
        recents = RecentCalls()
        callCenter = WatchCallCenter(bridge: bridge, recents: recents)
        favorites = WatchFavorites()
        phoneLink = PhoneLink(bridge: bridge, favorites: favorites)
        fritzBox = WatchFritzBox(bridge: bridge)
        // Caller names from the (cached) FRITZ!Box phonebook, also for a
        // push that launches the app.
        callCenter.nameLookup = { [fritzBox] number in fritzBox.name(for: number) }
        linkKey = AppGroup.containerURL.flatMap { try? DeepLinkKey.loadOrCreate(in: $0) }
        snapshot = WatchSnapshotPublisher(favorites: favorites, recents: recents, fritzBox: fritzBox, bridge: bridge)
    }

    func open(_ url: URL) {
        guard let link = DeepLink(url: url) else { return }
        handle(link, trusted: false)
    }

    /// Call links start only from the watch itself: complications carry
    /// the link key, App Shortcuts are `trusted`. There is no confirmation
    /// on the watch, so anything else is ignored. Other links just open the
    /// app on its home screen, which shows the missed calls.
    func handle(_ link: DeepLink, trusted: Bool) {
        guard case .call(let number, let name, let key) = link,
              trusted || DeepLinkKey.matches(key, expected: linkKey)
        else { return }
        Task { await callCenter.startCall(to: number, name: name) }
    }
}

final class WatchAppDelegate: NSObject, WKApplicationDelegate {
    func applicationDidFinishLaunching() {
        MainActor.assumeIsolated {
            _ = WatchServices.shared
        }
    }

    func applicationDidBecomeActive() {
        MainActor.assumeIsolated {
            let bridge = WatchServices.shared.bridge
            // Catch up on a registration that failed while offline.
            if bridge.isPaired, bridge.registration == .failed {
                Task { await bridge.registerDevice() }
            }
        }
    }
}
