import Foundation
import Testing
@testable import HousephoneKit

/// `welcome.profile` (v2.2, ADR-0008).
struct ProfileTests {
    @Test func welcomeFixtureCarriesTheProfile() throws {
        let message = try SignalingCoding.makeDecoder().decode(SignalingMessage.self, from: FixtureRoundTripTests.data("welcome.json"))
        guard case .welcome(let welcome) = message else {
            Issue.record("expected welcome")
            return
        }
        #expect(welcome.profile == BridgeProfile(id: "b", name: "Profil B", number: "030 1234568"))
        #expect(welcome.profile?.isWorthShowing == true)
    }

    @Test func welcomeFromABridgeWithoutProfiles() throws {
        let json = Data(#"{"type":"welcome","payload":{"bridgeId":"e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e","bridgeName":"Zuhause","bridgeVersion":"0.2.0","sipRegistered":true}}"#.utf8)
        let message = try SignalingCoding.makeDecoder().decode(SignalingMessage.self, from: json)
        guard case .welcome(let welcome) = message else {
            Issue.record("expected welcome")
            return
        }
        #expect(welcome.profile == nil)
    }

    @Test func singleProfileWithoutNumberIsNotShown() {
        #expect(!BridgeProfile(id: BridgeProfile.defaultID, name: "Standard").isWorthShowing)
        #expect(BridgeProfile(id: BridgeProfile.defaultID, name: "Profil A", number: "030 1234567").isWorthShowing)
        #expect(BridgeProfile(id: "b", name: "Profil B").isWorthShowing)
    }
}
