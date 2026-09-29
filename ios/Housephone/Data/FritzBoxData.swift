import Foundation
import HousephoneKit
import Observation

/// Phonebook and call list of the FRITZ!Box, fetched from the bridge over
/// HTTPS and cached on disk (signaling v1.2).
@MainActor
@Observable
final class FritzBoxData {
    let phonebook: FritzBoxResource<FritzBoxPhonebook>
    let history: FritzBoxResource<FritzBoxCallList>
    /// Caller names from the phonebook, rebuilt whenever it changes.
    private(set) var names: PhonebookNameIndex

    @ObservationIgnored private let bridge: BridgeConnection
    @ObservationIgnored private let http = BridgeHTTPClient()

    /// Entries fetched for the call list. The FRITZ!Box keeps a few
    /// hundred; the most recent ones are what people look at.
    static let historyLimit = 200

    init(bridge: BridgeConnection, directory: URL = URL.applicationSupportDirectory.appending(path: "FritzBox", directoryHint: .isDirectory)) {
        self.bridge = bridge
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
        bridge.welcome?.supports(.fritzboxPhonebook) == true || phonebook.value != nil
    }

    var showsHistory: Bool {
        bridge.welcome?.supports(.fritzboxHistory) == true || history.value != nil
    }

    /// Name for a number from the FRITZ!Box phonebook, if exactly one
    /// contact has it.
    func name(for number: String) -> String? {
        names.name(for: number)
    }

    func refreshPhonebook() async {
        guard let credentials = bridge.credentials else { return }
        let http = http
        await phonebook.refresh { etag in
            try await http.phonebook(ifNoneMatch: etag, credentials: credentials)
        }
    }

    func refreshHistory() async {
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
        if bridge.welcome?.supports(.fritzboxPhonebook) == true {
            await refreshPhonebook()
        }
    }

    /// Unpaired: the data belongs to the old bridge.
    func clear() {
        phonebook.clear()
        history.clear()
    }
}
