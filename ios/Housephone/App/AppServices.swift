import HousephoneKit
import Intents
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
    let admin: AdminCenter
    let appModel: AppModel
    let favorites: FavoritesStore
    let snapshot: SnapshotPublisher
    /// Proves that a call link comes from one of the app's own extensions
    /// (`DeepLinkKey`). `nil` without the App Group, e.g. unsigned builds:
    /// then every call link asks first.
    let linkKey: String?

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
        admin = AdminCenter(bridge: bridge)
        // The watch pairs itself as soon as the iPhone is connected.
        let callCenterOnConnected = bridge.onConnected
        bridge.onConnected = { [watchLink] in
            callCenterOnConnected?()
            watchLink.autoPairIfNeeded()
        }
        appModel = AppModel()
        favorites = FavoritesStore()
        linkKey = AppGroup.containerURL.flatMap { try? DeepLinkKey.loadOrCreate(in: $0) }
        snapshot = SnapshotPublisher(favorites: favorites, fritzBox: fritzBox, contacts: contacts, bridge: bridge, direct: direct, appModel: appModel, modelContainer: modelContainer)
        callCenter.onCallRecorded = { [snapshot] in snapshot.setNeedsPublish() }
        bridge.start()
    }

    // MARK: - Links from extensions, Siri and Shortcuts

    func open(_ url: URL) {
        if let link = DeepLink(url: url) {
            handle(link, trusted: false)
        } else {
            appModel.open(url)
        }
    }

    /// `trusted`: the request comes from the app itself (quick actions, App
    /// Intents). Call links from elsewhere start without asking only when
    /// they carry the link key.
    func handle(_ link: DeepLink, trusted: Bool) {
        switch link {
        case .call(let number, let name, let key):
            if trusted || DeepLinkKey.matches(key, expected: linkKey) {
                Task { await callCenter.startCall(to: number, name: name) }
            } else {
                // A name from a foreign link could be made up; show the number.
                appModel.callConfirmation = CallConfirmation(number: number)
            }
        case .keypad:
            appModel.selectedTab = .keypad
        case .recents(let missedOnly):
            appModel.recentsShowsMissedOnly = missedOnly
            appModel.selectedTab = .recents
        }
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

    /// "Hey Siri, ruf … mit Housephone an", also in CarPlay.
    func application(_ application: UIApplication, handlerFor intent: INIntent) -> Any? {
        intent is INStartCallIntent ? StartCallIntentHandler() : nil
    }

    /// A scene delegate for the home screen quick actions.
    func application(_ application: UIApplication, configurationForConnecting connectingSceneSession: UISceneSession, options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        let configuration = UISceneConfiguration(name: nil, sessionRole: connectingSceneSession.role)
        if connectingSceneSession.role == .windowApplication {
            configuration.delegateClass = SceneDelegate.self
        }
        return configuration
    }
}
