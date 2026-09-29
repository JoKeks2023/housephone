import Foundation
import HousephoneKit
import Observation

/// One entry in the watch's recents list.
struct RecentCall: Codable, Identifiable, Equatable {
    var id: UUID
    var number: String
    var name: String?
    var direction: CallDirection
    var outcome: CallOutcome
    var date: Date
    var duration: TimeInterval?

    init?(session: CallSession) {
        guard let outcome = session.outcome else { return nil }
        id = session.id.uuid
        number = session.remoteNumber
        name = session.remoteName
        direction = session.direction
        self.outcome = outcome
        date = session.createdAt
        duration = session.duration
    }

    var isMissed: Bool { direction == .incoming && outcome == .missed }
}

/// The last calls made or received on this watch. Small, so it lives as
/// JSON in the user defaults.
@MainActor
@Observable
final class RecentCalls {
    private(set) var calls: [RecentCall]

    private let defaults: UserDefaults
    private static let key = "recentCalls"
    private static let limit = 30

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        calls = (defaults.data(forKey: Self.key)).flatMap { try? JSONDecoder().decode([RecentCall].self, from: $0) } ?? []
    }

    func add(_ call: RecentCall) {
        calls.removeAll { $0.id == call.id }
        calls.insert(call, at: 0)
        if calls.count > Self.limit { calls.removeLast(calls.count - Self.limit) }
        if let data = try? JSONEncoder().encode(calls) {
            defaults.set(data, forKey: Self.key)
        }
    }

    /// The most recent number to call back, for the keypad.
    var lastNumber: String? {
        calls.first { !$0.number.isEmpty }?.number
    }
}
