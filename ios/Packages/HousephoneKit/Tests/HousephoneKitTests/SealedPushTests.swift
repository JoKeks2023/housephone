import CryptoKit
import Foundation
import Testing
@testable import HousephoneKit

/// Sealed pushes against `docs/protocol/fixtures/crypto/push-vectors.json`,
/// which the Go bridge seals with (ADR-0010).
struct SealedPushTests {
    static let vectors: [String: String] = {
        var url = URL(filePath: #filePath)
        for _ in 0..<6 { url.deleteLastPathComponent() }
        let file = url.appending(path: "docs/protocol/fixtures/crypto/push-vectors.json")
        return try! JSONDecoder().decode([String: String].self, from: Data(contentsOf: file))
    }()

    var v: [String: String] { Self.vectors }

    var deviceKey: Curve25519.KeyAgreement.PrivateKey {
        try! Curve25519.KeyAgreement.PrivateKey(rawRepresentation: HP2.data(base64URL: v["devicePrivateKey"]!)!)
    }

    var apnsBody: [AnyHashable: Any] {
        try! JSONSerialization.jsonObject(with: Data(v["apnsBody"]!.utf8)) as! [AnyHashable: Any]
    }

    @Test func publicKeyMatchesVector() {
        #expect(deviceKey.pushKeyWireValue == v["devicePublicKey"])
    }

    @Test func opensVector() throws {
        let plain = try SealedPush.open(v["sealed"]!, with: deviceKey)
        #expect(String(decoding: plain, as: UTF8.self) == v["plaintext"])
    }

    @Test func decodesSealedPushBody() throws {
        let push = try IncomingCallPush(dictionary: apnsBody, pushKey: deviceKey)
        #expect(push == (try IncomingCallPush(data: Data(v["plaintext"]!.utf8))))
        #expect(push.callerName == "Oma")
    }

    @Test func plaintextPushStillWorks() throws {
        let plain = try JSONSerialization.jsonObject(with: Data(v["plaintext"]!.utf8)) as! [AnyHashable: Any]
        let push = try IncomingCallPush(dictionary: plain, pushKey: nil)
        #expect(push.caller == "+4930123456")
    }

    @Test func tamperedPayloadFails() {
        var raw = HP2.data(base64URL: v["sealed"]!)!
        raw[raw.count - 1] ^= 1
        #expect(throws: SealedPushError.cannotOpen) { try SealedPush.open(raw, with: deviceKey) }
        var ephemeral = HP2.data(base64URL: v["sealed"]!)!
        ephemeral[0] ^= 1
        #expect(throws: SealedPushError.cannotOpen) { try SealedPush.open(ephemeral, with: deviceKey) }
    }

    @Test func wrongKeyFails() {
        #expect(throws: SealedPushError.cannotOpen) {
            try SealedPush.open(v["sealed"]!, with: Curve25519.KeyAgreement.PrivateKey())
        }
    }

    @Test func sealedWithoutKeyFails() {
        #expect(throws: SealedPushError.cannotOpen) { try IncomingCallPush(dictionary: apnsBody, pushKey: nil) }
    }

    @Test func malformedSealedValue() {
        #expect(throws: SealedPushError.malformed) { try SealedPush.open("not+base64url", with: deviceKey) }
        #expect(throws: SealedPushError.malformed) { try SealedPush.open(Data(count: 40), with: deviceKey) }
    }

    @Test func helloCarriesPushKey() throws {
        let hello = Hello(appVersion: "1", platform: .ios, pushKey: deviceKey.pushKeyWireValue)
        let data = try SignalingCoding.makeEncoder().encode(hello)
        let object = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        #expect(object?["pushKey"] as? String == v["devicePublicKey"])
    }
}
