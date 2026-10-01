import Foundation
import Testing
@testable import HousephoneKit

/// Discovery and IP-phone setup of the mode without bridge (HPHN-25, #30);
/// fictional boxes and numbers only.
struct FritzBoxSetupTests {
    // MARK: - Discovery

    static let description = """
    <?xml version="1.0"?>
    <root xmlns="urn:dslforum-org:device-1-0">
    <systemVersion><HW>200</HW><Major>154</Major><Minor>8</Minor><Patch>3</Patch><Display>154.08.03</Display></systemVersion>
    <device>
    <deviceType>urn:dslforum-org:device:InternetGatewayDevice:1</deviceType>
    <manufacturer>FRITZ! GmbH</manufacturer>
    <modelName>FRITZ!Box 7590 (Testnetz)</modelName>
    <serviceList>
    <service><serviceType>urn:dslforum-org:service:DeviceInfo:1</serviceType></service>
    <service><serviceType>urn:dslforum-org:service:X_VoIP:1</serviceType></service>
    </serviceList>
    </device>
    </root>
    """

    @Test func readsTheDeviceDescription() throws {
        let device = try #require(TR064XML.deviceDescription(Data(Self.description.utf8), host: "10.0.0.1"))
        #expect(device == FritzBoxDevice(host: "10.0.0.1", modelName: "FRITZ!Box 7590", osVersion: "8.03", supportsIPPhones: true))
        #expect(TR064XML.osVersion("161.08.25") == "8.25")
        #expect(TR064XML.osVersion("nonsense") == nil)
        #expect(TR064XML.displayModel("FRITZ!Box 6660 Cable") == "FRITZ!Box 6660 Cable")
    }

    @Test func rejectsOtherDevices() {
        let router = Self.description.replacingOccurrences(of: "FRITZ! GmbH", with: "Other Router Inc.")
        #expect(TR064XML.deviceDescription(Data(router.utf8), host: "10.0.0.1") == nil)
        #expect(TR064XML.deviceDescription(Data("<html/>".utf8), host: "10.0.0.1") == nil)
    }

    @Test func triesFritzBoxNameThenGatewaysThenFactoryAddress() async {
        let asked = Recorder<String>()
        let description = Data(Self.description.utf8)
        let discovery = FritzBoxDiscovery(
            fetch: { url in
                asked.append(url.host ?? "")
                guard url.host == "192.168.178.1", url.port == 49000, url.path == "/tr64desc.xml" else { throw URLError(.cannotConnectToHost) }
                return description
            },
            resolve: { name in name == "fritz.box" ? ["10.9.8.7"] : [] }
        )
        let device = await discovery.find(gateways: ["172.20.0.1", "10.9.8.7"])
        #expect(device?.host == "192.168.178.1")
        // Duplicates are asked once.
        #expect(asked.values == ["10.9.8.7", "172.20.0.1", "192.168.178.1"])
    }

    @Test func asksPrivateAddressesOnly() async {
        let asked = Recorder<String>()
        let discovery = FritzBoxDiscovery(
            fetch: { url in asked.append(url.host ?? ""); throw URLError(.cannotConnectToHost) },
            resolve: { _ in ["203.0.113.5"] }
        )
        #expect(await discovery.find(gateways: ["8.8.8.8", "fe80::1"]) == nil)
        #expect(asked.values == ["192.168.178.1"])
        #expect(FritzBoxDiscovery.isPrivateIPv4("172.31.255.1"))
        #expect(!FritzBoxDiscovery.isPrivateIPv4("172.32.0.1"))
        #expect(!FritzBoxDiscovery.isPrivateIPv4("192.168.0"))
        #expect(!FritzBoxDiscovery.isPrivateIPv4("192.168.0.300"))
    }

    // MARK: - X_VoIP documents

    static let numberList = """
    <List>
    <Item><Number>5550100</Number><Type>eVoIP</Type><Index>0</Index><Name>Festnetz</Name></Item>
    <Item><Number>5550199</Number><Type>eVoIP</Type><Index>1</Index><Name></Name></Item>
    <Item><Number>5550100</Number><Type>eISDN</Type><Index>0</Index><Name>ISDN1</Name></Item>
    </List>
    """

    @Test func readsNumbers() {
        let numbers = TR064XML.lineNumbers(Self.numberList)
        #expect(numbers.map(\.number) == ["5550100", "5550199", "5550100"])
        #expect(numbers[0] == FritzBoxLineNumber(number: "5550100", type: "eVoIP", index: 0, name: "Festnetz"))
        #expect(FritzBoxPhoneSetup.uniqueNumbers(numbers).map(\.number) == ["5550100", "5550199"])
        #expect(TR064XML.lineNumbers("") == [])
    }

    @Test func readsClients() {
        let xml = """
        <X_AVM-DE_ClientList><List>
        <Item>
        <X_AVM-DE_ClientIndex>0</X_AVM-DE_ClientIndex>
        <X_AVM-DE_ClientUsername>kueche</X_AVM-DE_ClientUsername>
        <X_AVM-DE_ClientRegistrar>192.0.2.1</X_AVM-DE_ClientRegistrar>
        <X_AVM-DE_PhoneName>Küche</X_AVM-DE_PhoneName>
        <X_AVM-DE_ClientId></X_AVM-DE_ClientId>
        <X_AVM-DE_OutGoingNumber>5550100</X_AVM-DE_OutGoingNumber>
        <X_AVM-DE_InComingNumbers>
        <Item><Number>5550100</Number><Type>eVoIP</Type><Index>0</Index><Name>Festnetz</Name></Item>
        </X_AVM-DE_InComingNumbers>
        <X_AVM-DE_ExternalRegistration>0</X_AVM-DE_ExternalRegistration>
        <X_AVM-DE_InternalNumber>620</X_AVM-DE_InternalNumber>
        </Item>
        <Item>
        <X_AVM-DE_ClientIndex>2</X_AVM-DE_ClientIndex>
        <X_AVM-DE_ClientUsername>housephone</X_AVM-DE_ClientUsername>
        <X_AVM-DE_PhoneName>Housephone (iPhone)</X_AVM-DE_PhoneName>
        <X_AVM-DE_ClientId>housephone-abc</X_AVM-DE_ClientId>
        <X_AVM-DE_OutGoingNumber></X_AVM-DE_OutGoingNumber>
        <X_AVM-DE_InComingNumbers></X_AVM-DE_InComingNumbers>
        <X_AVM-DE_InternalNumber>622</X_AVM-DE_InternalNumber>
        </Item>
        </List></X_AVM-DE_ClientList>
        """
        let clients = TR064XML.sipClients(xml)
        #expect(clients.count == 2)
        #expect(clients[0].index == 0 && clients[0].username == "kueche" && clients[0].internalNumber == "620")
        #expect(clients[0].incomingNumbers.map(\.number) == ["5550100"])
        #expect(clients[1].clientID == "housephone-abc" && clients[1].incomingNumbers.isEmpty)
    }

    @Test func writesIncomingNumbersLikeGetNumbers() {
        #expect(TR064XML.numberList([]) == "")
        let list = TR064XML.numberList([FritzBoxLineNumber(number: "5550100", type: "eVoIP", index: 0, name: "A&B")])
        #expect(list == "<List><Item><Number>5550100</Number><Type>eVoIP</Type><Index>0</Index><Name>A&amp;B</Name></Item></List>")
        #expect(TR064XML.lineNumbers(list).map(\.name) == ["A&B"])
    }

    @Test func putsTheTokenIntoTheSOAPHeader() {
        let envelope = TR064XML.envelope(action: "GetState", service: "urn:x", arguments: [], token: "2C0A2110-30BA")
        #expect(envelope.contains(#"<s:Header><avm:token xmlns:avm="avm.de" s:mustUnderstand="1">2C0A2110-30BA</avm:token></s:Header><s:Body>"#))
        #expect(!TR064XML.envelope(action: "GetState", service: "urn:x", arguments: []).contains("Header"))
    }

    @Test func mapsFaultCodes() {
        #expect(TR064XML.error(faultCode: "606", action: "a", status: 500) == .notAllowed)
        #expect(TR064XML.error(faultCode: "866", action: "a", status: 500) == .secondFactorRequired)
        #expect(TR064XML.error(faultCode: "867", action: "a", status: 500) == .secondFactorBlocked)
        #expect(TR064XML.error(faultCode: "868", action: "a", status: 500) == .secondFactorBusy)
        #expect(TR064XML.error(faultCode: "402", action: "a", status: 500) == .invalidResponse("a: HTTP 500, UPnP 402"))
        #expect(FritzBoxConfirmationMethod.parse("button,dtmf;*11234") == [.button, .dtmf("*11234")])
    }

    // MARK: - Setup

    static func client(_ index: Int, username: String, phoneName: String = "Telefon", clientID: String = "") -> FritzBoxSIPClient {
        FritzBoxSIPClient(index: index, username: username, phoneName: phoneName, clientID: clientID, outgoingNumber: "", incomingNumbers: [], internalNumber: "62\(index)")
    }

    @Test func surveyFindsOurClientByIDOrName() async throws {
        let box = FakeVoIPBox(clients: [Self.client(0, username: "kueche"), Self.client(3, username: "housephone", phoneName: "Housephone (iPhone)", clientID: "housephone-1")])
        let setup = FritzBoxPhoneSetup(service: box)
        #expect(try await setup.survey(clientID: "housephone-1", phoneName: "Anders").existing?.index == 3)
        #expect(try await setup.survey(clientID: nil, phoneName: "Housephone (iPhone)").existing?.index == 3)
        #expect(try await setup.survey(clientID: "other", phoneName: "Anders").existing == nil)
    }

    @Test func newClientTakesTheFirstFreeSlotAndAFreeUsername() throws {
        let survey = FritzBoxPhoneSetup.Survey(
            numbers: [],
            clients: [Self.client(0, username: "housephone"), Self.client(1, username: "Housephone2")],
            existing: nil,
            hasFreeSlot: true
        )
        let request = try FritzBoxPhoneSetup.request(survey: survey, reuse: false, phoneName: "Housephone (iPhone)", clientID: "housephone-1", outgoingNumber: "", incomingNumbers: [])
        #expect(request.index == 2)
        #expect(request.username == "housephone3")
        #expect(request.password.count == 32)
        #expect(request.password.allSatisfy { FritzBoxPhoneSetup.passwordAlphabet.contains($0) })
        #expect(FritzBoxPhoneSetup.makePassword() != FritzBoxPhoneSetup.makePassword())
    }

    @Test func reuseOverwritesTheExistingSlot() throws {
        let ours = Self.client(4, username: "housephone", phoneName: "Housephone (iPhone)", clientID: "housephone-1")
        let survey = FritzBoxPhoneSetup.Survey(numbers: [], clients: [ours], existing: ours, hasFreeSlot: true)
        let request = try FritzBoxPhoneSetup.request(survey: survey, reuse: true, phoneName: "Housephone (iPhone)", clientID: "housephone-1", outgoingNumber: "5550100", incomingNumbers: [], password: "fixed-password")
        #expect(request == FritzBoxSIPClientRequest(index: 4, username: "housephone", password: "fixed-password", phoneName: "Housephone (iPhone)", clientID: "housephone-1", outgoingNumber: "5550100", incomingNumbers: []))
    }

    @Test func fullBoxHasNoSlot() {
        let clients = (0..<10).map { Self.client($0, username: "t\($0)") }
        let survey = FritzBoxPhoneSetup.Survey(numbers: [], clients: clients, existing: nil, hasFreeSlot: false)
        #expect(throws: FritzBoxPhoneSetup.Failure.noFreeSlot) {
            try FritzBoxPhoneSetup.request(survey: survey, reuse: false, phoneName: "x", clientID: "y", outgoingNumber: "", incomingNumbers: [])
        }
        #expect(FritzBoxPhoneSetup.freeIndex(in: clients) == nil)
    }

    static let request = FritzBoxSIPClientRequest(index: 1, username: "housephone", password: "p", phoneName: "Housephone (iPhone)", clientID: "c", outgoingNumber: "", incomingNumbers: [])

    @Test func appliesWithoutConfirmation() async throws {
        let box = FakeVoIPBox(clients: [])
        let extensionNumber = try await FritzBoxPhoneSetup(service: box).apply(Self.request) { _ in Issue.record("no challenge expected") }
        #expect(extensionNumber == "621")
        #expect(box.writes == [nil])
    }

    @Test func confirmsAtTheBoxAndWritesWithTheToken() async throws {
        let box = FakeVoIPBox(clients: [], needsSecondFactor: true, states: [.waitingForAuth, .waitingForAuth, .authenticated])
        let shown = Recorder<FritzBoxSecondFactorChallenge>()
        let extensionNumber = try await FritzBoxPhoneSetup(service: box).apply(Self.request, pollInterval: .milliseconds(1)) { shown.append($0) }
        #expect(extensionNumber == "621")
        #expect(shown.values == [FritzBoxSecondFactorChallenge(token: "token-1", methods: [.button, .dtmf("*11234")], state: .waitingForAuth)])
        #expect(box.writes == [nil, "token-1"])
        #expect(box.stopped == 0)
    }

    @Test func givesUpWhenNotConfirmedInTime() async {
        let box = FakeVoIPBox(clients: [], needsSecondFactor: true, states: [])
        await #expect(throws: FritzBoxPhoneSetup.Failure.notConfirmed) {
            try await FritzBoxPhoneSetup(service: box).apply(Self.request, pollInterval: .milliseconds(1), timeout: .milliseconds(30)) { _ in }
        }
        #expect(box.stopped == 1)
        #expect(box.writes == [nil])
    }

    @Test func reportsABlockedConfirmation() async {
        let box = FakeVoIPBox(clients: [], needsSecondFactor: true, states: [.blocked])
        await #expect(throws: FritzBoxPhoneSetup.Failure.tr064(.secondFactorBlocked)) {
            try await FritzBoxPhoneSetup(service: box).apply(Self.request, pollInterval: .milliseconds(1)) { _ in }
        }
    }
}

final class Recorder<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: [Value] = []
    func append(_ value: Value) { lock.withLock { stored.append(value) } }
    var values: [Value] { lock.withLock { stored } }
}

/// Plays the FRITZ!Box's X_VoIP and X_AVM-DE_Auth services.
final class FakeVoIPBox: FritzBoxVoIPService, @unchecked Sendable {
    private let lock = NSLock()
    private let clients: [FritzBoxSIPClient]
    private let needsSecondFactor: Bool
    private var states: [FritzBoxSecondFactorState]
    private var confirmed = false
    private(set) var writes: [String?] = []
    private(set) var stopped = 0

    init(clients: [FritzBoxSIPClient], needsSecondFactor: Bool = false, states: [FritzBoxSecondFactorState] = []) {
        self.clients = clients
        self.needsSecondFactor = needsSecondFactor
        self.states = states
    }

    func numbers() async throws(TR064Error) -> [FritzBoxLineNumber] {
        TR064XML.lineNumbers(FritzBoxSetupTests.numberList)
    }

    func sipClients() async throws(TR064Error) -> [FritzBoxSIPClient] { clients }

    func setSIPClient(_ request: FritzBoxSIPClientRequest, token: String?) async throws(TR064Error) -> String {
        let allowed: Bool = lock.withLock {
            writes.append(token)
            return !needsSecondFactor || (confirmed && token == "token-1")
        }
        guard allowed else { throw .secondFactorRequired }
        return "62\(request.index)"
    }

    func startSecondFactor() async throws(TR064Error) -> FritzBoxSecondFactorChallenge {
        FritzBoxSecondFactorChallenge(token: "token-1", methods: [.button, .dtmf("*11234")], state: .waitingForAuth)
    }

    func secondFactorState(token: String) async throws(TR064Error) -> FritzBoxSecondFactorState {
        lock.withLock {
            guard !states.isEmpty else { return .waitingForAuth }
            let state = states.removeFirst()
            if state == .authenticated { confirmed = true }
            return state
        }
    }

    func stopSecondFactor(token: String) async {
        lock.withLock { stopped += 1 }
    }
}
