import Foundation
import HousephoneKit
import Observation

/// Phonebook and call list of the FRITZ!Box, fetched from the bridge over
/// HTTPS and cached on disk (signaling v1.2) — or, in the mode without
/// bridge, straight from the FRITZ!Box over TR-064 (ADR-0005).
@MainActor
@Observable
final class FritzBoxData {
    let phonebook: FritzBoxResource<FritzBoxPhonebook>
    let history: FritzBoxResource<FritzBoxCallList>
    /// Caller names from the phonebook, rebuilt whenever it changes.
    private(set) var names: PhonebookNameIndex

    @ObservationIgnored private let bridge: BridgeConnection
    @ObservationIgnored private let direct: DirectPhone
    @ObservationIgnored private let http = BridgeHTTPClient()

    /// Entries fetched for the call list. The FRITZ!Box keeps a few
    /// hundred; the most recent ones are what people look at.
    static let historyLimit = 200

    init(bridge: BridgeConnection, direct: DirectPhone, directory: URL = URL.applicationSupportDirectory.appending(path: "FritzBox", directoryHint: .isDirectory)) {
        self.bridge = bridge
        self.direct = direct
        phonebook = FritzBoxResource(cacheURL: directory.appending(path: "phonebook.json"))
        history = FritzBoxResource(cacheURL: directory.appending(path: "history.json"))
        names = PhonebookNameIndex(phonebook.value)
        phonebook.onValueChange = { [weak self] value in
            self?.names = PhonebookNameIndex(value)
        }
    }

    /// Show the FRITZ!Box phonebook: the bridge offers it, or there is a
    /// cached copy from before (e.g. while the bridge is unreachable).
    var showsPhonebook: Bool {
        bridge.welcome?.supports(.fritzboxPhonebook) == true || tr064 != nil || phonebook.value != nil
    }

    var showsHistory: Bool {
        bridge.welcome?.supports(.fritzboxHistory) == true || tr064 != nil || history.value != nil
    }

    /// Direct TR-064 access, when the mode without bridge has it set up.
    private var tr064: TR064Client? {
        direct.isEnabled ? direct.configuration?.tr064Client : nil
    }

    /// Name for a number from the FRITZ!Box phonebook, if exactly one
    /// contact has it.
    func name(for number: String) -> String? {
        names.name(for: number)
    }

    func refreshPhonebook() async {
        if let tr064 {
            await phonebook.refresh { _ in
                do {
                    return .updated(try await tr064.phonebook(), etag: nil)
                } catch {
                    throw FritzBoxUnavailableError(message: Self.message(for: error))
                }
            }
            return
        }
        guard let credentials = bridge.credentials else { return }
        let http = http
        await phonebook.refresh { etag in
            try await http.phonebook(ifNoneMatch: etag, credentials: credentials)
        }
    }

    func refreshHistory() async {
        if let tr064 {
            let limit = Self.historyLimit
            await history.refresh { _ in
                do {
                    return .updated(try await tr064.callList(limit: limit), etag: nil)
                } catch {
                    throw FritzBoxUnavailableError(message: Self.message(for: error))
                }
            }
            return
        }
        guard let credentials = bridge.credentials else { return }
        let http = http
        let limit = Self.historyLimit
        await history.refresh { _ in
            .updated(try await http.history(limit: limit, credentials: credentials), etag: nil)
        }
    }

    /// After (re)connecting: keep caller names current when the bridge
    /// offers the phonebook.
    func refreshAfterConnect() async {
        if bridge.welcome?.supports(.fritzboxPhonebook) == true || tr064 != nil {
            await refreshPhonebook()
        }
    }

    nonisolated static func message(for error: any Error) -> String {
        switch error as? TR064Error {
        case .unreachable: String(localized: "Die FRITZ!Box antwortet nicht. Bist du im Heim-WLAN?")
        case .authentication: String(localized: "Die FRITZ!Box hat die Anmeldung abgelehnt. Prüfe Benutzer und Kennwort für TR-064.")
        case .notAllowed: String(localized: "Dem FRITZ!Box-Benutzer fehlt das Recht „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“.")
        case .unsupported: String(localized: "Die FRITZ!Box bietet diese Funktion nicht an.")
        case .secondFactorRequired, .secondFactorBlocked, .secondFactorBusy: String(localized: "Die FRITZ!Box wartet auf eine Bestätigung. Versuche es gleich noch einmal.")
        case .invalidResponse, nil: String(localized: "Die FRITZ!Box hat unerwartet geantwortet.")
        }
    }

    /// Unpaired: the data belongs to the old bridge.
    func clear() {
        phonebook.clear()
        history.clear()
    }
}
