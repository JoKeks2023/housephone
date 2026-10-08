import HousephoneKit
import UIKit

/// Home screen quick actions: long-press the app icon to call a favorite.
@MainActor
enum QuickActions {
    private static var callType: String {
        (Bundle.main.bundleIdentifier ?? "housephone") + ".call"
    }

    static func item(for favorite: SharedSnapshot.Favorite) -> UIApplicationShortcutItem {
        let link = DeepLink.call(number: favorite.number, name: favorite.name, key: nil)
        return UIApplicationShortcutItem(
            type: callType,
            localizedTitle: favorite.name,
            localizedSubtitle: favorite.label ?? favorite.number,
            icon: UIApplicationShortcutIcon(systemImageName: "phone.fill"),
            userInfo: ["url": link.url.absoluteString as NSString]
        )
    }

    /// Quick actions only come from the app's own icon, so a call starts
    /// without asking.
    @discardableResult
    static func perform(_ item: UIApplicationShortcutItem) -> Bool {
        guard item.type == callType,
              let string = item.userInfo?["url"] as? String,
              let url = URL(string: string),
              let link = DeepLink(url: url)
        else { return false }
        AppServices.shared.handle(link, trusted: true)
        return true
    }
}

/// Only here for quick actions; SwiftUI still owns the window.
final class SceneDelegate: NSObject, UIWindowSceneDelegate {
    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options connectionOptions: UIScene.ConnectionOptions) {
        if let item = connectionOptions.shortcutItem {
            QuickActions.perform(item)
        }
    }

    func windowScene(_ windowScene: UIWindowScene, performActionFor shortcutItem: UIApplicationShortcutItem) async -> Bool {
        QuickActions.perform(shortcutItem)
    }
}
