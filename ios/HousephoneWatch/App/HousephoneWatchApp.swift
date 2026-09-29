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

    private init() {
        bridge = WatchBridge(store: KeychainCredentialStore())
        recents = RecentCalls()
        callCenter = WatchCallCenter(bridge: bridge, recents: recents)
        phoneLink = PhoneLink(bridge: bridge)
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
