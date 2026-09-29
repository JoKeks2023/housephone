import Foundation
import Testing
@testable import HousephoneKit

struct PairingLinkTests {
    static let fp = "If4x36FUomFia_hUBG_SJxt77UtqvkWqWId-9H-XIbk"

    @Test func parsesCanonicalLink() throws {
        let link = try PairingLink(string: "housephone://pair?v=2&url=wss%3A%2F%2Fphone.example.com%2Fv1%2Fws&code=K7P2XH9QRMW4DZT8&fp=\(Self.fp)&name=Zuhause%20Berlin")
        #expect(link.bridgeURL == URL(string: "wss://phone.example.com/v1/ws"))
        #expect(link.code == "K7P2XH9QRMW4DZT8")
        #expect(link.groupedCode == "K7P2-XH9Q-RMW4-DZT8")
        #expect(link.fingerprint == Self.fp)
        #expect(link.bridgeName == "Zuhause Berlin")
        #expect(link.isEncrypted)
    }

    @Test func normalizesCodeTypedByHand() throws {
        let link = try PairingLink(string: "  housephone://pair?v=2&url=wss://h.example/v1/ws&code=k7p2-xh9q%20rmw4-dzt8&fp=\(Self.fp) \n")
        #expect(link.code == "K7P2XH9QRMW4DZT8")
        #expect(link.bridgeName == nil)
    }

    @Test func allowsPlainWebSocketForLocalTests() throws {
        let link = try PairingLink(string: "housephone://pair?v=2&url=ws://192.168.178.20:8080/v1/ws&code=K7P2XH9QRMW4DZT8&fp=\(Self.fp)")
        #expect(!link.isEncrypted)
    }

    @Test(arguments: [
        ("https://example.com/pair?v=2&url=wss://h/v1/ws&code=K7P2XH9QRMW4DZT8&fp=\(fp)", PairingLinkError.notAPairingLink),
        ("housephone://settings?v=2&url=wss://h/v1/ws&code=K7P2XH9QRMW4DZT8&fp=\(fp)", .notAPairingLink),
        // v1 links (10-character code, no fingerprint) are outdated.
        ("housephone://pair?url=wss://h/v1/ws&code=K7P2XH9QRM", .outdatedLink),
        ("housephone://pair?v=1&url=wss://h/v1/ws&code=K7P2XH9QRMW4DZT8&fp=\(fp)", .outdatedLink),
        ("housephone://pair?v=2&code=K7P2XH9QRMW4DZT8&fp=\(fp)", .missingBridgeURL),
        ("housephone://pair?v=2&url=https://h/v1/ws&code=K7P2XH9QRMW4DZT8&fp=\(fp)", .invalidBridgeURL),
        ("housephone://pair?v=2&url=wss://h/v1/ws&fp=\(fp)", .missingCode),
        ("housephone://pair?v=2&url=wss://h/v1/ws&code=K7P2XH9QRMW4DZT0&fp=\(fp)", .invalidCode),
        ("housephone://pair?v=2&url=wss://h/v1/ws&code=K7P2XH9QRM&fp=\(fp)", .invalidCode),
        ("housephone://pair?v=2&url=wss://h/v1/ws&code=K7P2XH9QRMW4DZT8", .invalidFingerprint),
        ("housephone://pair?v=2&url=wss://h/v1/ws&code=K7P2XH9QRMW4DZT8&fp=If4x36FUomFia_hUBG_SJxt77UtqvkWqWId-9H-XIb", .invalidFingerprint),
        ("housephone://pair?v=2&url=wss://h/v1/ws&code=K7P2XH9QRMW4DZT8&fp=If4x36FUomFia_hUBG_SJxt77UtqvkWqWId-9H-XIbk=", .invalidFingerprint),
        ("housephone://pair?v=2&url=wss://h/v1/ws&code=K7P2XH9QRMW4DZT8&fp=If4x36FUomFia/hUBG+SJxt77UtqvkWqWId-9H-XIbk", .invalidFingerprint),
    ])
    func rejectsInvalidLinks(input: String, expected: PairingLinkError) {
        #expect(throws: expected) { try PairingLink(string: input) }
    }

    @Test func codeAlphabetExcludesLookalikes() {
        #expect(PairingCode.alphabet.count == 32)
        for character in "0O1I" {
            #expect(!PairingCode.alphabet.contains(character))
        }
        #expect(PairingCode.normalize("abcd efgh jkmn pqrs") == "ABCDEFGHJKMNPQRS")
        #expect(PairingCode.normalize("ABCD-EFGH-JKMN-PQRI") == nil)
        #expect(PairingCode.normalize("ABCDEFGHJK") == nil)
        #expect(PairingCode.grouped("ABCDEFGHJKMNPQRS") == "ABCD-EFGH-JKMN-PQRS")
    }

    @Test func credentialsCarryOnlyThePinnedKey() throws {
        let bridge = TestBridge()
        let credentials = TestDevice(bridge: bridge).credentials
        #expect(credentials.bridgeFingerprint == bridge.fingerprint)
        let json = try #require(String(data: try JSONEncoder().encode(credentials), encoding: .utf8))
        #expect(!json.lowercased().contains("secret"))
    }

    @Test func inMemoryStoreRoundTrips() throws {
        let store = InMemoryCredentialStore()
        #expect(try store.load() == nil)
        let credentials = TestDevice(bridge: TestBridge()).credentials
        try store.save(credentials)
        #expect(try store.load() == credentials)
        try store.delete()
        #expect(try store.load() == nil)
    }
}
