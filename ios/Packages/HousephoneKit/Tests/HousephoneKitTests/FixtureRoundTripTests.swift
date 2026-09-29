import Foundation
import Testing
@testable import HousephoneKit

/// Checks the Swift side against the shared fixtures in
/// `docs/protocol/fixtures`, which the Go bridge tests against as well.
struct FixtureRoundTripTests {
    static let fixturesDirectory: URL = {
        var url = URL(filePath: #filePath)
        // …/ios/Packages/HousephoneKit/Tests/HousephoneKitTests/<file>
        for _ in 0..<6 { url.deleteLastPathComponent() }
        return url.appending(path: "docs/protocol/fixtures", directoryHint: .isDirectory)
    }()

    static let pushFixture = "push.incoming_call.json"

    static var messageFixtures: [String] {
        let files = (try? FileManager.default.contentsOfDirectory(atPath: fixturesDirectory.path())) ?? []
        return files.filter { $0.hasSuffix(".json") && $0 != pushFixture }.sorted()
    }

    static func data(_ name: String) throws -> Data {
        try Data(contentsOf: fixturesDirectory.appending(path: name))
    }

    @Test func fixturesExist() {
        #expect(Self.messageFixtures.count == 20)
    }

    @Test(arguments: messageFixtures)
    func decodesAndReencodesIdentically(fixture: String) throws {
        let original = try Self.data(fixture)
        let message = try SignalingCoding.makeDecoder().decode(SignalingMessage.self, from: original)

        if case .unknown(let type) = message {
            Issue.record("\(fixture) decoded as unknown type \(type)")
        }
        #expect("\(message.type).json" == fixture)

        let reencoded = try SignalingCoding.makeEncoder().encode(message)
        let lhs = try JSONSerialization.jsonObject(with: original) as? NSDictionary
        let rhs = try JSONSerialization.jsonObject(with: reencoded) as? NSDictionary
        #expect(lhs != nil)
        #expect(lhs == rhs, "\(fixture) changed in round trip: \(String(decoding: reencoded, as: UTF8.self))")
    }

    @Test func everyMessageTypeHasAFixture() throws {
        let types = Set(try Self.messageFixtures.map {
            try SignalingCoding.makeDecoder().decode(SignalingMessage.self, from: Self.data($0)).type
        })
        let expected: Set = [
            "hello", "device.update", "device.unpair", "pair.companion.request", "pair.companion", "call.media", "call.attach", "call.dial", "call.answer", "call.accept",
            "call.hangup", "call.dtmf", "device.paired", "welcome", "status", "call.incoming", "call.offer",
            "call.state", "call.ended", "error",
        ]
        #expect(types == expected)
    }

    @Test func pairingFixturesDecode() throws {
        let request = try SignalingCoding.makeDecoder().decode(PairRequest.self, from: Self.data("http/pair.request.json"))
        #expect(request.code == "K7P2XH9QRMW4DZT8")
        #expect(request.platform == .ios)
        #expect(HP2.data(base64URL: request.publicKey)?.count == 65)
        #expect(HP2.data(base64URL: request.nonce)?.count == HP2.nonceLength)

        let response = try SignalingCoding.makeDecoder().decode(PairingResult.self, from: Self.data("http/pair.response.json"))
        #expect(response.deviceId.description == "9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d")
        #expect(HP2.data(base64URL: response.bridgePublicKey)?.count == 32)
    }

    @Test func devicePairedDecodes() throws {
        let message = try SignalingCoding.makeDecoder().decode(SignalingMessage.self, from: Self.data("device.paired.json"))
        guard case .devicePaired(let paired) = message else {
            Issue.record("expected device.paired")
            return
        }
        #expect(paired.platform == .watchos)
        #expect(paired.pairedAt == Date(timeIntervalSince1970: 1_790_705_045))
    }

    @Test func idsAreEncodedInLowercase() throws {
        let callId = CallID(UUID(uuidString: "3F0C2B4E-8A1D-4C6E-9B7A-2D5E8F1A0C93")!)
        let text = try SignalingCoding.encode(.callAttach(CallReference(callId: callId)))
        #expect(text.contains("3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93"))
    }

    @Test func unknownTypesAndFieldsAreTolerated() throws {
        let unknown = try SignalingCoding.decode(#"{"type":"call.hold","payload":{"callId":"x"}}"#)
        #expect(unknown == .unknown(type: "call.hold"))

        let extraField = try SignalingCoding.decode(#"{"type":"status","payload":{"sipRegistered":true,"future":1}}"#)
        #expect(extraField == .status(BridgeStatus(sipRegistered: true)))
    }

    @Test func unknownEndReasonMapsToFailure() throws {
        let message = try SignalingCoding.decode(
            #"{"type":"call.ended","payload":{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","reason":"gremlins"}}"#
        )
        guard case .callEnded(let ended) = message else {
            Issue.record("expected call.ended")
            return
        }
        #expect(ended.reason == .other("gremlins"))
        #expect(ended.reason.callKitReason(for: .incoming) == .failed)
    }

    @Test func acceptsFractionalSecondsFromGo() throws {
        let message = try SignalingCoding.decode(
            #"{"type":"call.incoming","payload":{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","caller":"0301","startedAt":"2026-09-29T18:04:05.123456789Z"}}"#
        )
        guard case .callIncoming(let call) = message else {
            Issue.record("expected call.incoming")
            return
        }
        #expect(abs(call.startedAt.timeIntervalSince1970 - 1_790_705_045.123) < 0.01)
    }

    @Test func offerWithoutIceServersDecodes() throws {
        let message = try SignalingCoding.decode(
            #"{"type":"call.offer","payload":{"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","sdp":"v=0"}}"#
        )
        guard case .callOffer(let offer) = message else {
            Issue.record("expected call.offer")
            return
        }
        #expect(offer.iceServers.isEmpty)
    }

    @Test func pushFixtureDecodes() throws {
        let object = try JSONSerialization.jsonObject(with: Self.data(Self.pushFixture)) as? [String: Any]
        var dictionary = try #require(object) as [AnyHashable: Any]
        dictionary["aps"] = [String: Any]()

        let push = try IncomingCallPush(dictionary: dictionary)
        #expect(push.callId.description == "3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93")
        #expect(push.caller == "+4930123456")
        #expect(push.callerName == "Oma")
        #expect(push.callerNumber == "+4930123456")
    }

    @Test func pushOfOtherTypeIsRejected() {
        let dictionary: [AnyHashable: Any] = [
            "type": "something_else", "v": 1, "callId": "3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93",
            "caller": "", "bridgeId": "b",
        ]
        #expect(throws: IncomingCallPushError.notAnIncomingCall(type: "something_else")) {
            try IncomingCallPush(dictionary: dictionary)
        }
    }

    @Test func withheldNumberHasNoCallerNumber() throws {
        let push = try IncomingCallPush(data: Data(
            #"{"type":"incoming_call","v":1,"callId":"3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93","caller":"","bridgeId":"b"}"#.utf8
        ))
        #expect(push.callerNumber == nil)
    }
}
