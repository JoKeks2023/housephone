import CryptoKit
import Foundation
import Testing
@testable import HousephoneKit

/// LAN pairing (ADR-0007) against
/// `docs/protocol/fixtures/crypto/lan-pairing-vectors.json`, which the Go
/// bridge tests against as well.
struct LanPairingVectorTests {
    struct Vectors: Decodable {
        var ids: [String: String]
        var urls: [String: String]
        var keys: [String: String]
        var steps: [String: String]
    }

    static let vectors: Vectors = {
        var url = URL(filePath: #filePath)
        for _ in 0..<6 { url.deleteLastPathComponent() }
        let file = url.appending(path: "docs/protocol/fixtures/crypto/lan-pairing-vectors.json")
        return try! JSONDecoder().decode(Vectors.self, from: Data(contentsOf: file))
    }()

    var v: Vectors { Self.vectors }
    var k: [String: String] { v.keys }
    var s: [String: String] { v.steps }

    let deviceKey = try! SoftwareDeviceKey(rawRepresentation: HP2VectorTests.hex("c9afa9d845ba75166b5c215767b1d6934e50c3db36e89b127b8a622b120f6721"))

    var offer: LanPairOffer {
        LanPairOffer(
            pairingId: v.ids["pairingId"]!, bridgeId: v.ids["bridgeId"]!, bridgeName: "Zuhause",
            bridgePublicKey: k["bridgePublicKey"]!, bridgeEphemeral: k["bridgeEphemeralPublic"]!,
            bridgeNonce: k["bridgeNonce"]!, expiresAt: Date(), signature: s["offerSignature"]!
        )
    }

    func pairing() throws -> HP2LanPairing {
        let ephemeral = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: HP2VectorTests.hex(k["deviceEphemeralPrivateX25519Hex"]!))
        return HP2LanPairing(key: deviceKey, deviceName: "iPhone", model: nil, ephemeral: ephemeral, nonce: HP2.data(base64URL: k["deviceNonce"]!)!)
    }

    @Test func messagesMatchTheBridge() throws {
        #expect(HP2.base64URL(deviceKey.publicKeyX963) == k["devicePublicKeyX963"])
        #expect(HP2.lanCommitment(publicKey: k["devicePublicKeyX963"]!, deviceEphemeral: k["deviceEphemeralPublic"]!, deviceNonce: k["deviceNonce"]!) == s["commitment"])
        let offerMessage = HP2.lanOfferMessage(
            pairingId: v.ids["pairingId"]!, bridgeId: v.ids["bridgeId"]!, publicKey: k["devicePublicKeyX963"]!,
            commitment: s["commitment"]!, bridgePublicKey: k["bridgePublicKey"]!,
            bridgeEphemeral: k["bridgeEphemeralPublic"]!, bridgeNonce: k["bridgeNonce"]!
        )
        #expect(String(decoding: offerMessage, as: UTF8.self) == s["offerMessage"])
        let hash = HP2.lanTranscriptHash(
            pairingId: v.ids["pairingId"]!, bridgeId: v.ids["bridgeId"]!, publicKey: k["devicePublicKeyX963"]!,
            commitment: s["commitment"]!, bridgePublicKey: k["bridgePublicKey"]!,
            bridgeEphemeral: k["bridgeEphemeralPublic"]!, bridgeNonce: k["bridgeNonce"]!,
            deviceEphemeral: k["deviceEphemeralPublic"]!, deviceNonce: k["deviceNonce"]!
        )
        #expect(HP2VectorTests.hex(hash) == s["transcriptHashHex"])
        #expect(String(decoding: HP2.lanProofMessage(transcriptHash: hash), as: UTF8.self) == s["proofMessage"])
        // The Go side's (random) proof verifies here.
        let proof = try P256.Signing.ECDSASignature(rawRepresentation: HP2.data(base64URL: s["proofSignature"]!)!)
        let publicKey = try P256.Signing.PublicKey(x963Representation: deviceKey.publicKeyX963)
        #expect(publicKey.isValidSignature(proof, for: HP2.lanProofMessage(transcriptHash: hash)))
        let approved = HP2.lanApprovedMessage(
            transcriptHash: hash, deviceId: v.ids["deviceId"]!, bridgeId: v.ids["bridgeId"]!,
            publicUrl: v.urls["publicUrl"]!, lanUrl: v.urls["lanUrl"]!
        )
        #expect(String(decoding: approved, as: UTF8.self) == s["approvedMessage"])
    }

    @Test func secretsMatchTheBridge() throws {
        let device = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: HP2VectorTests.hex(k["deviceEphemeralPrivateX25519Hex"]!))
        let bridge = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: HP2.data(base64URL: k["bridgeEphemeralPublic"]!)!)
        let shared = try device.sharedSecretFromKeyAgreement(with: bridge)
        #expect(shared.withUnsafeBytes { HP2VectorTests.hex(Data($0)) } == s["sharedSecretHex"])
        let secrets = HP2.lanSecrets(sharedSecret: shared, transcriptHash: HP2VectorTests.hex(s["transcriptHashHex"]!))
        #expect(secrets.sas == s["sas"])
        #expect(HP2VectorTests.hex(secrets.sealKey) == s["sealKeyHex"])
        #expect(try HP2.open(HP2VectorTests.hex(s["approvalSealedHex"]!), key: secrets.sealKey, counter: 0) == Data(s["approvalPlaintext"]!.utf8))
    }

    @Test func deviceSideEndToEnd() throws {
        var pairing = try pairing()
        #expect(pairing.start.commitment == s["commitment"])
        let reveal = try pairing.accept(offer, key: deviceKey)
        #expect(pairing.sas == s["sas"])
        #expect(reveal.deviceEphemeral == k["deviceEphemeralPublic"])
        #expect(reveal.deviceNonce == k["deviceNonce"])

        let credentials = try pairing.credentials(
            from: LanPairState(status: .approved, sealed: s["approvalSealed"]),
            discoveredURL: URL(string: "ws://192.168.178.20:8081/v1/ws")!,
            keyTag: "device-1"
        )
        #expect(credentials.deviceId.description == v.ids["deviceId"])
        #expect(credentials.bridgeURL.absoluteString == v.urls["publicUrl"])
        #expect(credentials.lanURL?.absoluteString == v.urls["lanUrl"])
        #expect(credentials.bridgeFingerprint == HP2.fingerprint(of: HP2.data(base64URL: k["bridgePublicKey"]!)!))
    }

    @Test func forgedOfferIsRefused() throws {
        var pairing = try pairing()
        var forged = offer
        forged.bridgeNonce = HP2.base64URL(Data(repeating: 7, count: 16))
        #expect(throws: HP2Error.bridgeIdentityMismatch) { try pairing.accept(forged, key: deviceKey) }
    }

    @Test func approvalUnderAnotherKeyIsRefused() throws {
        var pairing = try pairing()
        _ = try pairing.accept(offer, key: deviceKey)
        var sealed = HP2.data(base64URL: s["approvalSealed"]!)!
        sealed[0] ^= 1
        #expect(throws: HP2Error.bridgeIdentityMismatch) {
            try pairing.credentials(from: LanPairState(status: .approved, sealed: HP2.base64URL(sealed)), discoveredURL: URL(string: "ws://h:1/v1/ws")!, keyTag: "t")
        }
    }

    @Test(arguments: [(UInt32(0), "000000"), (42, "000042"), (999_999, "999999"), (4_294_967_295, "967295")])
    func formatsSAS(value: UInt32, text: String) {
        #expect(HP2.formatSAS(value) == text)
    }
}

/// The bridge's side of LAN pairing for the client tests, written against
/// the canonical messages only.
final class TestLanBridge: @unchecked Sendable {
    let bridge = TestBridge()
    let lock = NSLock()
    var outcome: LanPairState.Status = .approved
    var polls = 0
    private var start: LanPairStart?
    private var ephemeral = Curve25519.KeyAgreement.PrivateKey()
    private var hash = Data()
    private var sealKey: SymmetricKey?
    let pairingId = HP2.base64URL(Data(repeating: 0xA0, count: 16))
    let nonce = HP2.base64URL(Data(repeating: 0x20, count: 16))
    let deviceId = "0b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d"

    func handle(_ request: URLRequest, _ body: Data?) -> StubURLProtocol.Response {
        lock.withLock {
            let path = request.url!.path()
            let decoder = SignalingCoding.makeDecoder()
            let encoder = SignalingCoding.makeEncoder()
            let bridgeKey = HP2.base64URL(bridge.publicKey)
            let bridgeEph = HP2.base64URL(ephemeral.publicKey.rawRepresentation)
            if path == "/v1/pair/lan", let body, let start = try? decoder.decode(LanPairStart.self, from: body) {
                self.start = start
                let message = HP2.lanOfferMessage(pairingId: pairingId, bridgeId: bridge.bridgeId, publicKey: start.publicKey, commitment: start.commitment, bridgePublicKey: bridgeKey, bridgeEphemeral: bridgeEph, bridgeNonce: nonce)
                let offer = LanPairOffer(pairingId: pairingId, bridgeId: bridge.bridgeId, bridgeName: "Zuhause", bridgePublicKey: bridgeKey, bridgeEphemeral: bridgeEph, bridgeNonce: nonce, expiresAt: Date().addingTimeInterval(120), signature: HP2.base64URL(try! bridge.signingKey.signature(for: message)))
                return .init(status: 200, body: try! encoder.encode(offer))
            }
            if path.hasSuffix("/reveal"), let body, let start, let reveal = try? decoder.decode(LanPairReveal.self, from: body) {
                guard HP2.lanCommitment(publicKey: start.publicKey, deviceEphemeral: reveal.deviceEphemeral, deviceNonce: reveal.deviceNonce) == start.commitment else {
                    return .init(status: 403, body: Data(#"{"code":"pairing_invalid","message":"commitment"}"#.utf8))
                }
                hash = HP2.lanTranscriptHash(pairingId: pairingId, bridgeId: bridge.bridgeId, publicKey: start.publicKey, commitment: start.commitment, bridgePublicKey: bridgeKey, bridgeEphemeral: bridgeEph, bridgeNonce: nonce, deviceEphemeral: reveal.deviceEphemeral, deviceNonce: reveal.deviceNonce)
                let deviceKey = try! P256.Signing.PublicKey(x963Representation: HP2.data(base64URL: start.publicKey)!)
                let proof = try! P256.Signing.ECDSASignature(rawRepresentation: HP2.data(base64URL: reveal.proof)!)
                guard deviceKey.isValidSignature(proof, for: HP2.lanProofMessage(transcriptHash: hash)) else {
                    return .init(status: 403, body: Data(#"{"code":"pairing_invalid","message":"proof"}"#.utf8))
                }
                let peer = try! Curve25519.KeyAgreement.PublicKey(rawRepresentation: HP2.data(base64URL: reveal.deviceEphemeral)!)
                let shared = try! ephemeral.sharedSecretFromKeyAgreement(with: peer)
                sealKey = HP2.lanSecrets(sharedSecret: shared, transcriptHash: hash).sealKey
                return .init(status: 200, body: try! encoder.encode(LanPairState(status: .pending, expiresAt: Date().addingTimeInterval(120))))
            }
            if path == "/v1/pair/lan/\(pairingId)", let sealKey {
                polls += 1
                if polls == 1 {
                    return .init(status: 200, body: try! encoder.encode(LanPairState(status: .pending)))
                }
                guard outcome == .approved else { return .init(status: 200, body: try! encoder.encode(LanPairState(status: outcome))) }
                let message = HP2.lanApprovedMessage(transcriptHash: hash, deviceId: deviceId, bridgeId: bridge.bridgeId, publicUrl: "wss://phone.example.com/v1/ws", lanUrl: "ws://192.168.178.20:8081/v1/ws")
                let approval = LanPairApproval(deviceId: deviceId, bridgeId: bridge.bridgeId, bridgeName: "Zuhause", publicUrl: "wss://phone.example.com/v1/ws", lanUrl: "ws://192.168.178.20:8081/v1/ws", signature: HP2.base64URL(try! bridge.signingKey.signature(for: message)))
                let sealed = try! HP2.seal(try! JSONEncoder().encode(approval), key: sealKey, counter: 0)
                return .init(status: 200, body: try! encoder.encode(LanPairState(status: .approved, sealed: HP2.base64URL(sealed))))
            }
            return .init(status: 404, body: Data(#"{"code":"bad_request","message":"not found"}"#.utf8))
        }
    }
}

struct LanPairingClientTests {
    let lanURL = URL(string: "ws://192.168.178.20:8081/v1/ws")!

    @Test func pairsAfterApprovalAndPinsTheBridge() async throws {
        let bridge = TestLanBridge()
        let keyStore = InMemoryDeviceKeyStore()
        let client = BridgeHTTPClient(session: StubURLProtocol.session(bridge.handle), keyStore: keyStore)

        let session = try await client.startLanPairing(lanURL: lanURL, deviceName: "iPhone", model: "iPhone17,1")
        #expect(session.sas.count == 6 && session.sas.allSatisfy(\.isNumber))
        #expect(session.bridgeName == "Zuhause")
        let outcome = try await client.waitForLanApproval(session)
        guard case .approved(let credentials) = outcome else {
            Issue.record("outcome \(outcome)")
            return
        }
        #expect(credentials.bridgePublicKey == bridge.bridge.publicKey)
        #expect(credentials.bridgeURL == URL(string: "wss://phone.example.com/v1/ws"))
        #expect(credentials.lanURL == lanURL)
        #expect(credentials.deviceId.description == bridge.deviceId)
        #expect(try keyStore.key(tag: credentials.keyTag) != nil)
        #expect(bridge.polls == 2)
    }

    @Test(arguments: [LanPairState.Status.denied, .expired])
    func endsWithoutPairingAndDeletesTheKey(status: LanPairState.Status) async throws {
        let bridge = TestLanBridge()
        bridge.outcome = status
        let keyStore = InMemoryDeviceKeyStore()
        let client = BridgeHTTPClient(session: StubURLProtocol.session(bridge.handle), keyStore: keyStore)
        let session = try await client.startLanPairing(lanURL: lanURL, deviceName: "iPhone", model: nil)
        let outcome = try await client.waitForLanApproval(session)
        #expect(outcome == (status == .denied ? .denied : .expired))
        #expect(keyStore.tags.isEmpty)
    }

    @Test func refusedOutsideTheHomeNetwork() async {
        let body = Data(#"{"code":"home_network_required","message":"pairing is only possible in the home network"}"#.utf8)
        let keyStore = InMemoryDeviceKeyStore()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { _, _ in .init(status: 403, body: body) }, keyStore: keyStore)
        await #expect(throws: BridgeHTTPError.homeNetworkRequired) {
            try await client.startLanPairing(lanURL: lanURL, deviceName: "iPhone", model: nil)
        }
        #expect(keyStore.tags.isEmpty)
    }

    @Test func impostorOfferLeavesNoKey() async {
        // Someone answers in the bridge's name but signs with another key
        // than the one it claims.
        let impostor = TestLanBridge()
        let keyStore = InMemoryDeviceKeyStore()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { request, body in
            var response = impostor.handle(request, body)
            if request.url!.path() == "/v1/pair/lan", var offer = try? SignalingCoding.makeDecoder().decode(LanPairOffer.self, from: response.body) {
                offer.bridgePublicKey = HP2.base64URL(Curve25519.Signing.PrivateKey().publicKey.rawRepresentation)
                response.body = try! SignalingCoding.makeEncoder().encode(offer)
            }
            return response
        }, keyStore: keyStore)
        await #expect(throws: HP2Error.bridgeIdentityMismatch) {
            try await client.startLanPairing(lanURL: lanURL, deviceName: "iPhone", model: nil)
        }
        #expect(keyStore.tags.isEmpty)
    }
}
