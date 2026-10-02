import CryptoKit
import Foundation
import Testing
@testable import HousephoneKit

/// Administration from the app (ADR-0009) against
/// `docs/protocol/fixtures/crypto/admin-vectors.json`, which the Go bridge
/// checks as well, and the client against a stub bridge.
struct AdminTests {
    struct Vectors: Decodable {
        var ids: [String: String]
        var keys: [String: String]
        var request: [String: String]
        var enroll: [String: String]
    }

    static let vectors: Vectors = {
        var url = URL(filePath: #filePath)
        for _ in 0..<6 { url.deleteLastPathComponent() }
        let file = url.appending(path: "docs/protocol/fixtures/crypto/admin-vectors.json")
        return try! JSONDecoder().decode(Vectors.self, from: Data(contentsOf: file))
    }()

    var v: Vectors { Self.vectors }

    private var adminKey: SoftwareDeviceKey {
        get throws {
            let scalar = try #require(Data(hexString: v.keys["adminPrivateScalarHex"]!))
            return try SoftwareDeviceKey(rawRepresentation: scalar)
        }
    }

    private func publicKey(_ b64: String) throws -> P256.Signing.PublicKey {
        try P256.Signing.PublicKey(x963Representation: try #require(HP2.data(base64URL: b64)))
    }

    private func signature(_ b64: String) throws -> P256.Signing.ECDSASignature {
        try P256.Signing.ECDSASignature(rawRepresentation: try #require(HP2.data(base64URL: b64)))
    }

    @Test func adminMessageMatchesTheBridge() throws {
        let r = v.request
        let message = HP2.adminMessage(
            method: r["method"]!, pathAndQuery: r["path"]!, bridgeId: v.ids["bridgeId"]!, deviceId: v.ids["deviceId"]!,
            timestamp: Int(r["ts"]!)!, nonce: r["nonce"]!, ephemeralPublicKey: r["epk"]!, body: Data(r["body"]!.utf8)
        )
        #expect(String(decoding: message, as: UTF8.self) == r["adminMessage"])
        // The Go bridge's signature verifies with CryptoKit.
        let key = try publicKey(v.keys["adminPublicKeyX963"]!)
        #expect(key.isValidSignature(try signature(r["adminSignature"]!), for: message))
        // It is not valid as the device's HP2-AUTH message.
        let auth = HP2.authMessage(
            method: r["method"]!, pathAndQuery: r["path"]!, bridgeId: v.ids["bridgeId"]!, deviceId: v.ids["deviceId"]!,
            timestamp: Int(r["ts"]!)!, nonce: r["nonce"]!, ephemeralPublicKey: r["epk"]!, body: Data(r["body"]!.utf8)
        )
        #expect(!key.isValidSignature(try signature(r["adminSignature"]!), for: auth))
    }

    @Test func enrollmentMatchesTheBridge() throws {
        let key = try adminKey
        #expect(HP2.base64URL(key.publicKeyX963) == v.keys["adminPublicKeyX963"])
        let message = HP2.adminEnrollMessage(bridgeId: v.ids["bridgeId"]!, deviceId: v.ids["deviceId"]!, adminKey: v.keys["adminPublicKeyX963"]!)
        #expect(String(decoding: message, as: UTF8.self) == v.enroll["enrollMessage"])
        #expect(try publicKey(v.keys["adminPublicKeyX963"]!).isValidSignature(try signature(v.enroll["proof"]!), for: message))

        let credentials = BridgeCredentials(
            bridgeURL: URL(string: "wss://phone.example.com/v1/ws")!,
            deviceId: DeviceID(UUID(uuidString: v.ids["deviceId"]!)!),
            bridgeId: v.ids["bridgeId"]!, bridgeName: "Zuhause", bridgePublicKey: Data(count: 32), keyTag: "device"
        )
        let enrollment = try AdminEnrollment.make(key: key, credentials: credentials)
        #expect(enrollment.adminKey == v.keys["adminPublicKeyX963"])
        #expect(try publicKey(enrollment.adminKey).isValidSignature(try signature(enrollment.proof), for: message))
    }

    @Test func roleWindow() {
        let now = Date(timeIntervalSince1970: 1_790_000_000)
        #expect(AdminRole(admin: true, enrolled: false, enrollUntil: now.addingTimeInterval(60)).canEnroll(at: now))
        #expect(!AdminRole(admin: true, enrolled: false, enrollUntil: now.addingTimeInterval(-1)).canEnroll(at: now))
        #expect(!AdminRole(admin: true, enrolled: true).canEnroll(at: now))
        #expect(!AdminRole(admin: false, enrolled: false, enrollUntil: now.addingTimeInterval(60)).canEnroll(at: now))
    }

    // MARK: - Client

    let bridge = TestBridge()

    private func lanDevice() -> TestDevice {
        TestDevice(bridge: bridge, lanURL: "ws://192.168.178.20:8081/v1/ws")
    }

    /// Admin requests go to the private listener and carry both signatures.
    @Test func adminRequestsAreSignedTwiceAndGoToTheLAN() async throws {
        let device = lanDevice()
        let admin = SoftwareDeviceKey()
        let log = RequestLog()
        let verified = RequestLog()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { [bridge] request, body in
            log.record(request, body)
            let path = HP2Signer.pathAndQuery(of: request.url!)
            let ok = bridge.verifiesDevice(
                authorization: request.value(forHTTPHeaderField: "Authorization") ?? "",
                method: request.httpMethod ?? "", pathAndQuery: path, body: body ?? Data(), devicePublicKey: device.key.publicKeyX963
            ) && bridge.verifiesAdmin(
                request: request, pathAndQuery: path, body: body ?? Data(), adminPublicKey: admin.publicKeyX963
            )
            if ok { verified.record(request, body) }
            let json = Data(#"{"id":"3c4d","name":"Küche","platform":"ios","keyFingerprint":"AB","profile":"default","profileName":"Profil A","createdAt":"2026-10-01T18:00:00Z","online":false}"#.utf8)
            return bridge.stubResponse(for: request, status: 200, json: json)
        }, keyStore: device.keyStore)

        let renamed = try await client.adminRename("3c4d", to: "Küche", key: admin, credentials: device.credentials)
        #expect(renamed.name == "Küche")
        let request = try #require(log.last)
        #expect(request.url.absoluteString == "http://192.168.178.20:8081/v1/admin/devices/3c4d")
        #expect(request.method == "PUT")
        #expect(verified.last != nil, "the bridge could verify both signatures")
    }

    @Test func removeSendsTheQueryInTheSignedPath() async throws {
        let device = lanDevice()
        let admin = SoftwareDeviceKey()
        let verified = RequestLog()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { [bridge] request, body in
            let path = HP2Signer.pathAndQuery(of: request.url!)
            if path == "/v1/admin/devices/3c4d?keepCompanions=1",
               bridge.verifiesAdmin(request: request, pathAndQuery: path, body: body ?? Data(), adminPublicKey: admin.publicKeyX963) {
                verified.record(request, body)
            }
            return bridge.stubResponse(for: request, status: 200, json: Data(#"{"removed":[]}"#.utf8))
        }, keyStore: device.keyStore)
        _ = try await client.adminRemove("3c4d", keepCompanions: true, key: admin, credentials: device.credentials)
        #expect(verified.last != nil)
    }

    @Test func withoutLanURLThereIsNoAdmin() async throws {
        let device = TestDevice(bridge: bridge)
        let client = BridgeHTTPClient(session: StubURLProtocol.session { _, _ in .init(status: 500, body: Data()) }, keyStore: device.keyStore)
        await #expect(throws: BridgeAdminError.noHomeNetworkAddress) {
            _ = try await client.adminStatus(key: SoftwareDeviceKey(), credentials: device.credentials)
        }
    }

    @Test func refusalIsReportedAsBridgeError() async throws {
        let device = lanDevice()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { [bridge] request, _ in
            bridge.stubResponse(for: request, status: 403, json: Data(#"{"code":"admin_required","message":"admin role and Face ID signature required"}"#.utf8))
        }, keyStore: device.keyStore)
        do {
            _ = try await client.adminDevices(key: SoftwareDeviceKey(), credentials: device.credentials)
            Issue.record("expected an error")
        } catch SignalingClientError.bridge(let payload) {
            #expect(payload.code == .adminRequired)
        }
    }

    @Test func enrollSendsTheProofWithoutAdminHeader() async throws {
        let device = lanDevice()
        let admin = SoftwareDeviceKey()
        let log = RequestLog()
        let client = BridgeHTTPClient(session: StubURLProtocol.session { [bridge] request, body in
            log.record(request, body)
            return bridge.stubResponse(for: request, status: 200, json: Data(#"{"admin":true,"enrolled":true}"#.utf8))
        }, keyStore: device.keyStore)
        let role = try await client.enrollAdmin(key: admin, credentials: device.credentials)
        #expect(role == AdminRole(admin: true, enrolled: true))
        let request = try #require(log.last)
        #expect(request.url.absoluteString == "http://192.168.178.20:8081/v1/admin/enroll")
        #expect(request.headers[HP2.adminHeaderName] == nil)
        let sent = try JSONDecoder().decode(AdminEnrollment.self, from: try #require(request.body))
        #expect(sent.adminKey == HP2.base64URL(admin.publicKeyX963))
    }
}

extension TestBridge {
    /// Whether the request's `HP2-Admin` header is the admin key's
    /// signature over it.
    func verifiesAdmin(request: URLRequest, pathAndQuery: String, body: Data, adminPublicKey: Data) -> Bool {
        guard let auth = HP2Authorization(headerValue: request.value(forHTTPHeaderField: "Authorization") ?? ""),
              let value = request.value(forHTTPHeaderField: HP2.adminHeaderName),
              let signature = HP2.data(base64URL: value).flatMap({ try? P256.Signing.ECDSASignature(rawRepresentation: $0) }),
              let key = try? P256.Signing.PublicKey(x963Representation: adminPublicKey)
        else { return false }
        let message = HP2.adminMessage(
            method: request.httpMethod ?? "", pathAndQuery: pathAndQuery, bridgeId: bridgeId, deviceId: auth.deviceId,
            timestamp: auth.timestamp, nonce: auth.nonce, ephemeralPublicKey: auth.ephemeralPublicKey, body: body
        )
        return key.isValidSignature(signature, for: message)
    }
}

private extension Data {
    init?(hexString: String) {
        var data = Data()
        var index = hexString.startIndex
        while index < hexString.endIndex {
            let next = hexString.index(index, offsetBy: 2)
            guard let byte = UInt8(hexString[index..<next], radix: 16) else { return nil }
            data.append(byte)
            index = next
        }
        self = data
    }
}
