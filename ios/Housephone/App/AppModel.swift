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
        case .missingBridgeURL, .invalidBridgeURL: "Der Link enthält keine gültige Bridge-Adresse."
        case .missingCode, .invalidCode: "Der Kopplungscode im Link ist ungültig."
        case .outdatedLink: "Dieser Link stammt von einer älteren Bridge. Aktualisiere die Bridge und erzeuge einen neuen Code."
        case .invalidFingerprint: "Dem Link fehlt die Kennung der Bridge. Erzeuge einen neuen Code."
        }
    }
}
