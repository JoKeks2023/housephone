import Foundation
import Testing
@testable import HousephoneKit

/// Phonebook and call list (signaling v1.2) against the shared fixtures in
/// `docs/protocol/fixtures/http`, which the bridge produces as well.
struct FritzBoxFixtureTests {
    static let httpFixtures: URL = {
        var url = URL(filePath: #filePath)
        // …/ios/Packages/HousephoneKit/Tests/HousephoneKitTests/<file>
        for _ in 0..<6 { url.deleteLastPathComponent() }
        return url.appending(path: "docs/protocol/fixtures/http", directoryHint: .isDirectory)
    }()

    static func data(_ name: String) throws -> Data {
        try Data(contentsOf: httpFixtures.appending(path: name))
    }

    static func roundTrip<T: Codable>(_ type: T.Type, fixture: String) throws -> T {
        let original = try data(fixture)
        let value = try SignalingCoding.makeDecoder().decode(T.self, from: original)
        let reencoded = try SignalingCoding.makeEncoder().encode(value)
        let lhs = try JSONSerialization.jsonObject(with: original) as? NSDictionary
        let rhs = try JSONSerialization.jsonObject(with: reencoded) as? NSDictionary
        #expect(lhs != nil)
        #expect(lhs == rhs, "\(fixture) changed in round trip: \(String(decoding: reencoded, as: UTF8.self))")
        return value
    }

    @Test func phonebookFixtureRoundTrips() throws {
        let phonebook = try Self.roundTrip(FritzBoxPhonebook.self, fixture: "phonebook.json")
        #expect(phonebook.contacts.count == 2)

        let grandma = try #require(phonebook.contacts.first { $0.id == "0-1234" })
        #expect(grandma.name == "Oma")
        #expect(grandma.favorite)
        #expect(grandma.phonebook == "Telefonbuch")
        #expect(grandma.numbers.map(\.type) == [.home, .mobile])
        #expect(grandma.primaryNumber?.number == "030123456")

        let doorphone = try #require(phonebook.contacts.first { $0.id == "0-1236" })
        #expect(doorphone.numbers.first?.type == .intern)
        #expect(doorphone.primaryNumber?.number == "**620")
    }

    @Test func historyFixtureRoundTrips() throws {
        let history = try Self.roundTrip(FritzBoxCallList.self, fixture: "history.json")
        #expect(history.calls.map(\.id) == ["2513", "2512", "2511", "2510"])

        let outgoing = history.calls[0]
        #expect(outgoing.direction == .outgoing)
        #expect(outgoing.result == .answered)
        #expect(outgoing.answeredBy == nil)
        #expect(outgoing.durationSeconds == 120)

        let withheld = history.calls[2]
        #expect(withheld.isMissed)
        #expect(withheld.number.isEmpty)
        #expect(withheld.name == nil)
        #expect(withheld.device == nil)

        let machine = history.calls[3]
        #expect(machine.isAnsweringMachine)
        #expect(machine.device == "Anrufbeantworter 1")
        #expect(machine.startedAt == ISO8601DateFormatter().date(from: "2026-09-28T19:45:00Z"))
    }

    @Test func missedCallsSince() throws {
        let history = try SignalingCoding.makeDecoder().decode(FritzBoxCallList.self, from: Self.data("history.json"))
        let since = try #require(ISO8601DateFormatter().date(from: "2026-09-29T00:00:00Z"))
        #expect(history.missedCalls(since: since).map(\.id) == ["2511"])
        #expect(history.missedCalls(since: .distantFuture).isEmpty)
    }

    @Test func unknownValuesSurviveDecoding() throws {
        let json = #"""
        {"updatedAt":"2026-09-29T18:04:05Z","calls":[{"id":"1","direction":"sideways","result":"forwarded","number":"1","answeredBy":"robot","startedAt":"2026-09-29T18:04:05Z","durationSeconds":0}]}
        """#
        let history = try SignalingCoding.makeDecoder().decode(FritzBoxCallList.self, from: Data(json.utf8))
        #expect(history.calls[0].direction.rawValue == "sideways")
        #expect(history.calls[0].result.rawValue == "forwarded")
        #expect(history.calls[0].answeredBy?.rawValue == "robot")
        #expect(!history.calls[0].isMissed)

        let phonebookJSON = #"{"updatedAt":"2026-09-29T18:04:05Z","contacts":[{"id":"1-1","name":"X","favorite":false,"numbers":[{"number":"1","type":"pager","preferred":false}]}]}"#
        let phonebook = try SignalingCoding.makeDecoder().decode(FritzBoxPhonebook.self, from: Data(phonebookJSON.utf8))
        #expect(phonebook.contacts[0].numbers[0].type.rawValue == "pager")
        #expect(phonebook.contacts[0].phonebook == nil)
    }

    @Test func welcomeAnnouncesFeatures() throws {
        let data = try Data(contentsOf: FixtureRoundTripTests.fixturesDirectory.appending(path: "welcome.json"))
        guard case .welcome(let welcome) = try SignalingCoding.makeDecoder().decode(SignalingMessage.self, from: data) else {
            Issue.record("welcome.json is not a welcome message")
            return
        }
        #expect(welcome.supports(.fritzboxPhonebook))
        #expect(welcome.supports(.fritzboxHistory))

        let old = Welcome(bridgeId: "b", bridgeName: "n", bridgeVersion: "0.1.0", sipRegistered: true)
        #expect(!old.supports(.fritzboxPhonebook))
        let encoded = try SignalingCoding.encode(.welcome(old))
        #expect(!encoded.contains("features"), "v1.1 welcome must not grow a features field")
    }
}

struct PhonebookNameIndexTests {
    static func phonebook(_ contacts: [(String, [String])]) -> FritzBoxPhonebook {
        FritzBoxPhonebook(updatedAt: .now, contacts: contacts.enumerated().map { index, entry in
            FritzBoxContact(
                id: "0-\(index)",
                name: entry.0,
                favorite: false,
                phonebook: "Telefonbuch",
                numbers: entry.1.map { FritzBoxNumber(number: $0, type: .home, preferred: false) }
            )
        })
    }

    @Test(arguments: ["030123456", "+4930123456", "004930123456", "+49 30 123456", "030/123 456"])
    func findsNameInEveryNotation(number: String) {
        let index = PhonebookNameIndex(Self.phonebook([("Oma", ["+4930123456"])]))
        #expect(index.name(for: number) == "Oma")
    }

    @Test func ambiguousNumberHasNoName() {
        let index = PhonebookNameIndex(Self.phonebook([("Oma", ["030123456"]), ("Opa", ["+4930123456"])]))
        #expect(index.name(for: "030123456") == nil)
    }

    @Test func sameNameInTwoPhonebooksStaysKnown() {
        let index = PhonebookNameIndex(Self.phonebook([("Oma", ["030123456"]), ("Oma", ["030123456"])]))
        #expect(index.name(for: "030123456") == "Oma")
    }

    @Test func foreignAndInternalNumbers() {
        let index = PhonebookNameIndex(Self.phonebook([("Tante Wien", ["+43 1 234567"]), ("Tür", ["**620"])]))
        #expect(index.name(for: "0043 1 234567") == "Tante Wien")
        #expect(index.name(for: "01234567") == nil)
        #expect(index.name(for: "**620") == nil)
        #expect(index.name(for: "") == nil)
    }

    @Test func emptyWithoutPhonebook() {
        #expect(PhonebookNameIndex(nil).isEmpty)
        #expect(PhonebookNameIndex(nil).name(for: "030123456") == nil)
    }
}

struct FritzBoxHTTPTests {
    let bridge = TestBridge()
    let device: TestDevice

    init() {
        device = TestDevice(bridge: bridge)
    }

    var credentials: BridgeCredentials { device.credentials }

    func client(_ handler: @escaping StubURLProtocol.Handler) -> BridgeHTTPClient {
        BridgeHTTPClient(session: StubURLProtocol.session(handler), keyStore: device.keyStore)
    }

    @Test func fetchesPhonebookWithETag() async throws {
        let log = RequestLog()
        let body = try FritzBoxFixtureTests.data("phonebook.json")
        let client = client { [bridge] request, requestBody in
            log.record(request, requestBody)
            return bridge.stubResponse(for: request, status: 200, json: body, extraHeaders: ["ETag": #""pb-7""#])
        }
        let result = try await client.phonebook(ifNoneMatch: nil, credentials: credentials)

        guard case .updated(let phonebook, let etag) = result else {
            Issue.record("expected an updated phonebook")
            return
        }
        #expect(phonebook.contacts.count == 2)
        #expect(etag == #""pb-7""#)
        let request = try #require(log.last)
        #expect(request.method == "GET")
        #expect(request.url.absoluteString == "https://phone.example.com/v1/phonebook")
        #expect(request.headers["Authorization"]?.hasPrefix("HP2 ") == true)
        #expect(request.headers["If-None-Match"] == nil)
    }

    @Test func notModifiedWhenETagMatches() async throws {
        let log = RequestLog()
        let client = client { [bridge] request, body in
            log.record(request, body)
            return request.value(forHTTPHeaderField: "If-None-Match") == #""pb-7""#
                ? bridge.stubResponse(for: request, status: 304)
                : bridge.stubResponse(for: request, status: 500)
        }
        let result = try await client.phonebook(ifNoneMatch: #""pb-7""#, credentials: credentials)
        guard case .notModified = result else {
            Issue.record("expected notModified")
            return
        }
        #expect(log.last?.headers["If-None-Match"] == #""pb-7""#)
    }

    @Test func unsignedNotModifiedIsUntrusted() async {
        let client = client { _, _ in .init(status: 304, body: Data()) }
        await #expect(throws: SignalingClientError.untrustedBridge) {
            try await client.phonebook(ifNoneMatch: #""pb-7""#, credentials: credentials)
        }
    }

    @Test func fritzBoxUnavailableCarriesMessage() async {
        let client = client { [bridge] request, _ in
            bridge.stubResponse(for: request, status: 503, json: Data(#"{"code":"fritzbox_unavailable","message":"TR-064 ist nicht eingerichtet"}"#.utf8))
        }
        do {
            _ = try await client.phonebook(ifNoneMatch: nil, credentials: credentials)
            Issue.record("expected an error")
        } catch SignalingClientError.bridge(let payload) {
            #expect(payload.code == .fritzboxUnavailable)
            #expect(FritzBoxResource<FritzBoxPhonebook>.failure(for: SignalingClientError.bridge(payload)) == .unavailable(message: "TR-064 ist nicht eingerichtet"))
        } catch {
            Issue.record("unexpected error \(error)")
        }
    }

    @Test(arguments: [(100, "100"), (0, "1"), (9_999, "500")])
    func fetchesHistoryWithClampedLimit(limit: Int, expected: String) async throws {
        let log = RequestLog()
        let verified = RequestLog()
        let body = try FritzBoxFixtureTests.data("history.json")
        let client = client { [bridge, device] request, requestBody in
            log.record(request, requestBody)
            // The signature covers the query as sent.
            if bridge.verifiesDevice(
                authorization: request.value(forHTTPHeaderField: "Authorization") ?? "",
                method: "GET", pathAndQuery: "/v1/history?limit=\(expected)", body: Data(), devicePublicKey: device.key.publicKeyX963
            ) {
                verified.record(request, requestBody)
            }
            return bridge.stubResponse(for: request, status: 200, json: body)
        }
        let history = try await client.history(limit: limit, credentials: credentials)
        #expect(history.calls.count == 4)
        let url = try #require(log.last?.url)
        #expect(url.path() == "/v1/history")
        #expect(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems == [URLQueryItem(name: "limit", value: expected)])
        #expect(verified.last != nil)
    }
}

@MainActor
struct FritzBoxResourceTests {
    let directory = FileManager.default.temporaryDirectory.appending(path: "fritzbox-\(UUID().uuidString)", directoryHint: .isDirectory)
    var cacheURL: URL { directory.appending(path: "phonebook.json") }

    static let phonebook = FritzBoxPhonebook(
        updatedAt: Date(timeIntervalSince1970: 1_800_000_000),
        contacts: [FritzBoxContact(id: "0-1", name: "Oma", favorite: true, phonebook: nil, numbers: [FritzBoxNumber(number: "030123456", type: .home, preferred: true)])]
    )

    /// Counts calls and hands out prepared answers.
    final class FakeBridge: @unchecked Sendable {
        private let lock = NSLock()
        private var answers: [Result<FritzBoxFetchResult<FritzBoxPhonebook>, any Error>]
        private(set) var seenETags: [String?] = []
        var delay: Duration = .zero

        init(_ answers: [Result<FritzBoxFetchResult<FritzBoxPhonebook>, any Error>]) {
            self.answers = answers
        }

        var fetch: FritzBoxResource<FritzBoxPhonebook>.Fetch {
            { etag in
                let (answer, delay) = self.lock.withLock {
                    self.seenETags.append(etag)
                    return (self.answers.removeFirst(), self.delay)
                }
                if delay > .zero { try await Task.sleep(for: delay) }
                return try answer.get()
            }
        }

        var calls: Int { lock.withLock { seenETags.count } }
    }

    @Test func cachesAcrossLaunches() async throws {
        defer { try? FileManager.default.removeItem(at: directory) }
        let bridge = FakeBridge([.success(.updated(Self.phonebook, etag: "e1"))])
        let resource = FritzBoxResource<FritzBoxPhonebook>(cacheURL: cacheURL)
        var changes = 0
        resource.onValueChange = { _ in changes += 1 }

        await resource.refresh(bridge.fetch)
        #expect(resource.value == Self.phonebook)
        #expect(resource.status == .current)
        #expect(resource.fetchedAt != nil)
        #expect(changes == 1)

        // Next launch: the cached copy is there before any network.
        let relaunched = FritzBoxResource<FritzBoxPhonebook>(cacheURL: cacheURL)
        #expect(relaunched.value == Self.phonebook)
        #expect(relaunched.status == .idle)

        // …and it revalidates with the stored ETag.
        let later = FakeBridge([.success(.notModified)])
        await relaunched.refresh(later.fetch)
        #expect(later.seenETags == ["e1"])
        #expect(relaunched.value == Self.phonebook)
        #expect(relaunched.status == .current)
    }

    @Test func failureKeepsTheCachedValue() async throws {
        defer { try? FileManager.default.removeItem(at: directory) }
        let bridge = FakeBridge([
            .success(.updated(Self.phonebook, etag: nil)),
            .failure(URLError(.notConnectedToInternet)),
            .failure(SignalingClientError.bridge(SignalingErrorPayload(code: .fritzboxUnavailable, message: "FRITZ!Box antwortet nicht"))),
            .failure(SignalingClientError.unauthorized),
        ])
        let resource = FritzBoxResource<FritzBoxPhonebook>(cacheURL: cacheURL)
        await resource.refresh(bridge.fetch)
        let confirmedAt = resource.fetchedAt

        await resource.refresh(bridge.fetch)
        #expect(resource.status == .failed(.unreachable))
        #expect(resource.value == Self.phonebook)
        #expect(resource.fetchedAt == confirmedAt)

        await resource.refresh(bridge.fetch)
        #expect(resource.failure == .unavailable(message: "FRITZ!Box antwortet nicht"))

        await resource.refresh(bridge.fetch)
        #expect(resource.failure == .unauthorized)
        #expect(bridge.seenETags == [nil, nil, nil, nil], "no ETag was sent by the bridge, so none is replayed")
    }

    @Test func concurrentRefreshesShareOneRequest() async {
        let bridge = FakeBridge([.success(.updated(Self.phonebook, etag: "e"))])
        bridge.delay = .milliseconds(50)
        let resource = FritzBoxResource<FritzBoxPhonebook>(cacheURL: nil)
        async let first: Void = resource.refresh(bridge.fetch)
        async let second: Void = resource.refresh(bridge.fetch)
        _ = await (first, second)
        #expect(bridge.calls == 1)
        #expect(resource.value == Self.phonebook)
    }

    @Test func clearForgetsEverythingAndDropsARunningRefresh() async throws {
        defer { try? FileManager.default.removeItem(at: directory) }
        let bridge = FakeBridge([
            .success(.updated(Self.phonebook, etag: "e")),
            .success(.updated(Self.phonebook, etag: "e2")),
        ])
        let resource = FritzBoxResource<FritzBoxPhonebook>(cacheURL: cacheURL)
        await resource.refresh(bridge.fetch)
        #expect(FileManager.default.fileExists(atPath: cacheURL.path()))

        resource.clear()
        #expect(resource.value == nil)
        #expect(resource.fetchedAt == nil)
        #expect(!FileManager.default.fileExists(atPath: cacheURL.path()))

        // Unpairing while a refresh runs must not bring the data back.
        bridge.delay = .milliseconds(50)
        async let running: Void = resource.refresh(bridge.fetch)
        try await Task.sleep(for: .milliseconds(10))
        resource.clear()
        await running
        #expect(resource.value == nil)
        #expect(resource.status == .idle)
    }
}
