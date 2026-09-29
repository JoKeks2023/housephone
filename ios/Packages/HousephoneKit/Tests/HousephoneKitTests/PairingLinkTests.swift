import Foundation
import Testing
@testable import HousephoneKit

struct PairingLinkTests {
    @Test func parsesCanonicalLink() throws {
        let link = try PairingLink(string: "housephone://pair?url=wss%3A%2F%2Fphone.example.com%2Fv1%2Fws&code=K7P2XH9QRM&name=Zuhause%20Berlin")
        #expect(link.bridgeURL == URL(string: "wss://phone.example.com/v1/ws"))
        #expect(link.code == "K7P2XH9QRM")
        #expect(link.bridgeName == "Zuhause Berlin")
        #expect(link.isEncrypted)
    }

    @Test func normalizesCodeTypedByHand() throws {
        let link = try PairingLink(string: "  housephone://pair?url=wss://h.example/v1/ws&code=k7p2-xh9q%20rm \n")
        #expect(link.code == "K7P2XH9QRM")
        #expect(link.bridgeName == nil)
    }

    @Test func allowsPlainWebSocketForLocalTests() throws {
        let link = try PairingLink(string: "housephone://pair?url=ws://192.168.178.20:8080/v1/ws&code=K7P2XH9QRM")
        #expect(!link.isEncrypted)
    }

    @Test(arguments: [
        ("https://example.com/pair?url=wss://h/v1/ws&code=K7P2XH9QRM", PairingLinkError.notAPairingLink),
        ("housephone://settings?url=wss://h/v1/ws&code=K7P2XH9QRM", .notAPairingLink),
        ("housephone://pair?code=K7P2XH9QRM", .missingBridgeURL),
        ("housephone://pair?url=https://h/v1/ws&code=K7P2XH9QRM", .invalidBridgeURL),
        ("housephone://pair?url=wss://h/v1/ws", .missingCode),
        ("housephone://pair?url=wss://h/v1/ws&code=K7P2XH9QR0", .invalidCode),
        ("housephone://pair?url=wss://h/v1/ws&code=SHORT", .invalidCode),
    ])
    func rejectsInvalidLinks(input: String, expected: PairingLinkError) {
        #expect(throws: expected) { try PairingLink(string: input) }
    }

    @Test func codeAlphabetExcludesLookalikes() {
        #expect(PairingCode.alphabet.count == 32)
        for character in "0O1I" {
            #expect(!PairingCode.alphabet.contains(character))
        }
        #expect(PairingCode.normalize("abcd efgh jk") == "ABCDEFGHJK")
        #expect(PairingCode.normalize("ABCDEFGHIJ") == nil)
    }

    @Test func credentialsBuildBearerHeader() throws {
        let deviceId = try #require(DeviceID(string: "9B1D4C2A-5E6F-4A7B-8C9D-0E1F2A3B4C5D"))
        let credentials = BridgeCredentials(
            bridgeURL: URL(string: "wss://h/v1/ws")!, deviceId: deviceId, deviceSecret: "secret", bridgeId: "b", bridgeName: "Zuhause"
        )
        #expect(credentials.authorizationHeader == "Bearer 9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d.secret")
    }

    @Test func inMemoryStoreRoundTrips() throws {
        let store = InMemoryCredentialStore()
        #expect(try store.load() == nil)
        let credentials = BridgeCredentials(bridgeURL: URL(string: "wss://h/v1/ws")!, deviceId: DeviceID(), deviceSecret: "s", bridgeId: "b", bridgeName: "n")
        try store.save(credentials)
        #expect(try store.load() == credentials)
        try store.delete()
        #expect(try store.load() == nil)
    }
}
