import Foundation
import HousephoneKit
import Observation

/// UI state that outlives individual screens.
@MainActor
@Observable
final class AppModel {
    enum Tab: Hashable {
        case recents
        case keypad
        case contacts
        case settings
    }

    var selectedTab: Tab = .keypad
    var keypadNumber = ""
    /// The call screen is minimized to the pill above the tab bar, e.g.
    /// to look up a number during a call. Resets with every new call.
    var isCallMinimized = false
    /// When the recents were last looked at: missed calls after this count
    /// as new (tab badge, status card).
    var recentsSeenAt: Date = UserDefaults.standard.object(forKey: "recents.seenAt") as? Date ?? .distantPast {
        didSet { UserDefaults.standard.set(recentsSeenAt, forKey: "recents.seenAt") }
    }
    /// The recents filter, so "missed" can be opened from elsewhere.
    var recentsShowsMissedOnly = false

    /// Minimizing needs the tab bar accessory to bring the call back.
    static var canMinimizeCall: Bool {
        if #available(iOS 26.1, *) { true } else { false }
    }
    /// A pairing link opened via `housephone://pair?…` or scanned.
    var pairingLink: PairingLink?
    var pairingLinkError: PairingLinkError?

    func open(_ url: URL) {
        do {
            pairingLink = try PairingLink(url: url)
        } catch {
            pairingLinkError = error
        }
    }
}

extension PairingLink: @retroactive Identifiable {
    public var id: String { "\(bridgeURL.absoluteString)#\(code)" }
}

extension PairingLinkError {
    var message: LocalizedStringResource {
        switch self {
        case .notAPairingLink: "Das ist kein Housephone-Kopplungslink."
        case .missingBridgeURL, .invalidBridgeURL, .invalidLanURL: "Der Link enthält keine gültige Bridge-Adresse."
        case .missingCode, .invalidCode: "Der Kopplungscode im Link ist ungültig."
        case .outdatedLink: "Dieser Link stammt von einer älteren Bridge. Aktualisiere die Bridge und erzeuge einen neuen Code."
        case .invalidFingerprint: "Dem Link fehlt die Kennung der Bridge. Erzeuge einen neuen Code."
        }
    }
}
