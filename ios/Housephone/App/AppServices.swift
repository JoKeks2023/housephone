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
    let callCenter: CallCenter
    let appModel: AppModel

    private init() {
        modelContainer = Self.makeModelContainer()
        contacts = ContactsDirectory()
        bridge = BridgeConnection(store: KeychainCredentialStore())
        callCenter = CallCenter(bridge: bridge, contacts: contacts, modelContainer: modelContainer)
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
