import Foundation
import Testing
@testable import HousephoneKit

struct AppGroupTests {
    @Test(arguments: [
        ("com.example.housephone", "group.com.example.housephone"),
        ("com.example.housephone.widgets", "group.com.example.housephone"),
        ("com.example.housephone.watchkitapp", "group.com.example.housephone"),
        ("org.housephone.fork.housephone.widgets", "group.org.housephone.fork.housephone"),
    ])
    func derivesGroupFromBundleID(bundleID: String, expected: String) {
        #expect(AppGroup.identifier(forBundleIdentifier: bundleID) == expected)
    }

    @Test(arguments: ["com.example.telephone", "housephone", ""])
    func rejectsForeignBundleIDs(bundleID: String) {
        #expect(AppGroup.identifier(forBundleIdentifier: bundleID) == nil)
    }
}

struct SharedSnapshotTests {
    private static func call(_ minutesAgo: Double, missed: Bool, now: Date) -> SharedSnapshot.RecentCall {
        SharedSnapshot.RecentCall(
            id: UUID().uuidString,
            number: "030 123456",
            name: "Test",
            direction: .incoming,
            outcome: missed ? .missed : .answered,
            date: now.addingTimeInterval(-minutesAgo * 60)
        )
    }

    @Test func countsMissedCallsSinceLastSeen() {
        let now = Date()
        let snapshot = SharedSnapshot(
            recentCalls: [
                Self.call(1, missed: true, now: now),
                Self.call(5, missed: false, now: now),
                Self.call(30, missed: true, now: now),
            ],
            recentsSeenAt: now.addingTimeInterval(-10 * 60)
        )
        #expect(snapshot.newMissedCalls.count == 1)
    }

    @Test func keepsOnlyTheNewestCalls() {
        let now = Date()
        let calls = (0..<50).map { Self.call(Double($0), missed: false, now: now) }
        let snapshot = SharedSnapshot(recentCalls: calls)
        #expect(snapshot.recentCalls.count == SharedSnapshot.recentCallsLimit)
        #expect(snapshot.recentCalls.first == calls.first)
    }

    @Test func roundTripsThroughTheStore() throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = SharedSnapshotStore(directory: directory)
        #expect(store.load() == nil)

        let snapshot = SharedSnapshot(
            favorites: [.init(id: UUID(), name: "Test", number: "030 123456", label: "Privat")],
            recentCalls: [Self.call(2, missed: true, now: Date(timeIntervalSince1970: 1_800_000_000))],
            recentsSeenAt: Date(timeIntervalSince1970: 1_700_000_000),
            isSetUp: true
        )
        #expect(try store.save(snapshot))
        #expect(store.load() == snapshot)
        // Unchanged: nothing is written, so widgets are not reloaded.
        #expect(try store.save(snapshot) == false)
    }
}

struct NameMatcherTests {
    private let names = ["Anna Müller", "Anna Schmidt", "Oma", "Praxis Dr. Weber", "Annabell"]

    private func match(_ query: String) -> [String] {
        NameMatcher.matches(query, in: names) { $0 }
    }

    @Test func exactNameWins() {
        #expect(match("oma") == ["Oma"])
        #expect(match("annabell") == ["Annabell"])
    }

    @Test func ignoresCaseAndDiacritics() {
        #expect(match("anna mueller").isEmpty)
        #expect(match("ANNA MULLER") == ["Anna Müller"])
    }

    @Test func everyWordMustStartANameWord() {
        #expect(match("anna") == ["Anna Müller", "Anna Schmidt", "Annabell"])
        #expect(match("anna s") == ["Anna Schmidt"])
        #expect(match("weber") == ["Praxis Dr. Weber"])
        #expect(match("dr weber") == ["Praxis Dr. Weber"])
    }

    @Test func emptyQueryFindsNothing() {
        #expect(match("").isEmpty)
        #expect(match("  ").isEmpty)
    }
}
