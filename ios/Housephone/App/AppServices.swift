import HousephoneKit
import os
import SwiftData
import UIKit

/// Long-lived objects of the app. Created during launch — also when iOS
/// launches the app in the background for a VoIP push — so PushKit and
/// CallKit are ready before the first push is delivered.
@MainActor
final class AppServices {
    static let shared = AppServices()

    let modelContainer: ModelContainer
    let contacts: ContactsDirectory
    let bridge: BridgeConnection
    let direct: DirectPhone
    let fritzBox: FritzBoxData
    let callCenter: CallCenter
    let watchLink: WatchLink
    let appModel: AppModel

    private init() {
        modelContainer = Self.makeModelContainer()
        contacts = ContactsDirectory()
        bridge = BridgeConnection(store: KeychainCredentialStore())
        direct = DirectPhone()
        fritzBox = FritzBoxData(bridge: bridge, direct: direct)
        // Caller names for numbers only the FRITZ!Box phonebook knows — also
        // for a push that launches the app, from the cached phonebook.
        contacts.fallbackName = { [fritzBox] number in fritzBox.name(for: number) }
        callCenter = CallCenter(bridge: bridge, direct: direct, contacts: contacts, modelContainer: modelContainer)
        watchLink = WatchLink(bridge: bridge)
        // The watch pairs itself as soon as the iPhone is connected.
        let callCenterOnConnected = bridge.onConnected
        bridge.onConnected = { [watchLink] in
            callCenterOnConnected?()
            watchLink.autoPairIfNeeded()
        }
        appModel = AppModel()
        bridge.start()
    }

    private static func makeModelContainer() -> ModelContainer {
        do {
            return try ModelContainer(for: CallRecord.self)
        } catch {
            // The recents list is a convenience; never let it block calls.
            Logger(subsystem: "com.jorisconrad.housephone", category: "storage")
                .error("Recents store unavailable, using memory: \(error.localizedDescription, privacy: .public)")
            let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
            // An in-memory store for a single, simple model cannot fail.
            return try! ModelContainer(for: CallRecord.self, configurations: configuration)
        }
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        _ = AppServices.shared
        return true
    }
}
