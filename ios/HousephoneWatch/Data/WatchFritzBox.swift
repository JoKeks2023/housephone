import Foundation
import HousephoneKit
import Observation

/// FRITZ!Box phonebook and call list on the watch. Fetched over HTTPS,
/// which watchOS allows at any time, and cached for offline use.
@MainActor
@Observable
final class WatchFritzBox {
    let phonebook: FritzBoxResource<FritzBoxPhonebook>
    let history: FritzBoxResource<FritzBoxCallList>
    private(set) var names: PhonebookNameIndex

    @ObservationIgnored private let bridge: WatchBridge
    @ObservationIgnored private let http = BridgeHTTPClient()

    /// The home screen only shows the last day's missed calls.
    static let historyLimit = 50

    init(bridge: WatchBridge, directory: URL = URL.applicationSupportDirectory.appending(path: "FritzBox", directoryHint: .isDirectory)) {
        self.bridge = bridge
        phonebook = FritzBoxResource(cacheURL: directory.appending(path: "phonebook.json"))
        history = FritzBoxResource(cacheURL: directory.appending(path: "history.json"))
        names = PhonebookNameIndex(phonebook.value)
        phonebook.onValueChange = { [weak self] value in
            self?.names = PhonebookNameIndex(value)
        }
    }

    func name(for number: String) -> String? {
        names.name(for: number)
    }

    /// Missed calls of the last 24 hours, newest first.
    func recentMissedCalls(now: Date = .now) -> [FritzBoxCall] {
        history.value?.missedCalls(since: now.addingTimeInterval(-24 * 60 * 60)) ?? []
    }

    func refreshPhonebook() async {
        guard bridge.isPaired, let credentials = bridge.credentials else { return }
        let http = http
        await phonebook.refresh { etag in
            try await http.phonebook(ifNoneMatch: etag, credentials: credentials)
        }
    }

    func refreshHistory() async {
        guard bridge.isPaired, let credentials = bridge.credentials else { return }
        let http = http
        let limit = Self.historyLimit
        await history.refresh { _ in
            .updated(try await http.history(limit: limit, credentials: credentials), etag: nil)
        }
    }

    func refreshAll() async {
        await refreshPhonebook()
        await refreshHistory()
    }

    /// Unpaired: the data belongs to the old bridge.
    func clear() {
        phonebook.clear()
        history.clear()
    }
}
