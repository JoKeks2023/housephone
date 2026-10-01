import Foundation

// Creating the IP phone for the mode without bridge over TR-064 (HPHN-25).
// Spec: FRITZ! "TR-064 Support – X_VoIP" v71 (2025-08-08) and
// "TR-064 Support – Authentication" v5 (2025-08-07), fritz.support/resources.

/// A telephone number of the FRITZ!Box (`X_AVM-DE_GetNumbers`).
public struct FritzBoxLineNumber: Hashable, Sendable, Identifiable {
    public var number: String
    /// `eVoIP`, `eISDN`, `ePOTS`, `eGSM`, …
    public var type: String
    public var index: Int
    public var name: String

    public var id: String { "\(type)-\(index)-\(number)" }

    public init(number: String, type: String, index: Int, name: String) {
        self.number = number
        self.type = type
        self.index = index
        self.name = name
    }
}

/// An IP phone configured on the FRITZ!Box (`X_AVM-DE_GetClients`).
public struct FritzBoxSIPClient: Hashable, Sendable {
    /// 0 … 9.
    public var index: Int
    public var username: String
    public var phoneName: String
    public var clientID: String
    public var outgoingNumber: String
    /// Empty: rings for all numbers.
    public var incomingNumbers: [FritzBoxLineNumber]
    /// 620 … 629.
    public var internalNumber: String

    public init(index: Int, username: String, phoneName: String, clientID: String, outgoingNumber: String, incomingNumbers: [FritzBoxLineNumber], internalNumber: String) {
        self.index = index
        self.username = username
        self.phoneName = phoneName
        self.clientID = clientID
        self.outgoingNumber = outgoingNumber
        self.incomingNumbers = incomingNumbers
        self.internalNumber = internalNumber
    }
}

/// What `X_AVM-DE_SetClient4` writes.
public struct FritzBoxSIPClientRequest: Equatable, Sendable {
    public var index: Int
    public var username: String
    public var password: String
    public var phoneName: String
    public var clientID: String
    /// Empty: the FRITZ!Box takes the first available number.
    public var outgoingNumber: String
    /// Empty: rings for all numbers.
    public var incomingNumbers: [FritzBoxLineNumber]
}

/// State of a second-factor confirmation (`X_AVM-DE_Auth`, StateEnum).
public enum FritzBoxSecondFactorState: String, Sendable {
    case disabled
    case waitingForAuth = "waitingforauth"
    case anotherAuthProcess = "anotherauthprocess"
    case authenticated
    case stopped
    case blocked
    case failure
}

/// How the user confirms: press a button on the box, or dial a code on a
/// phone connected to it (`X_AVM-DE_Auth` 6.2).
public enum FritzBoxConfirmationMethod: Hashable, Sendable {
    case button
    case dtmf(String)

    /// "button,dtmf;*11234" → [.button, .dtmf("*11234")].
    static func parse(_ text: String) -> [FritzBoxConfirmationMethod] {
        text.split(separator: ",").compactMap { part in
            let item = part.trimmingCharacters(in: .whitespaces)
            if item == "button" { return .button }
            if item.hasPrefix("dtmf;") {
                let code = String(item.dropFirst(5))
                return code.isEmpty ? nil : .dtmf(code)
            }
            return nil
        }
    }
}

public struct FritzBoxSecondFactorChallenge: Equatable, Sendable {
    public var token: String
    public var methods: [FritzBoxConfirmationMethod]
    public var state: FritzBoxSecondFactorState?
}

/// The TR-064 calls the setup needs, so the flow can be tested without a box.
public protocol FritzBoxVoIPService: Sendable {
    func numbers() async throws(TR064Error) -> [FritzBoxLineNumber]
    func sipClients() async throws(TR064Error) -> [FritzBoxSIPClient]
    /// Returns the internal number (62x).
    func setSIPClient(_ request: FritzBoxSIPClientRequest, token: String?) async throws(TR064Error) -> String
    func startSecondFactor() async throws(TR064Error) -> FritzBoxSecondFactorChallenge
    func secondFactorState(token: String) async throws(TR064Error) -> FritzBoxSecondFactorState
    func stopSecondFactor(token: String) async
}

extension TR064Client: FritzBoxVoIPService {
    public func numbers() async throws(TR064Error) -> [FritzBoxLineNumber] {
        let values = try await call(Self.voipControl, Self.voipService, "X_AVM-DE_GetNumbers")
        return TR064XML.lineNumbers(values["NewNumberList"] ?? "")
    }

    public func sipClients() async throws(TR064Error) -> [FritzBoxSIPClient] {
        let values = try await call(Self.voipControl, Self.voipService, "X_AVM-DE_GetClients")
        return TR064XML.sipClients(values["NewX_AVM-DE_ClientList"] ?? "")
    }

    public func setSIPClient(_ request: FritzBoxSIPClientRequest, token: String?) async throws(TR064Error) -> String {
        let values = try await call(Self.voipControl, Self.voipService, "X_AVM-DE_SetClient4", [
            ("NewX_AVM-DE_ClientIndex", String(request.index)),
            ("NewX_AVM-DE_ClientPassword", request.password),
            ("NewX_AVM-DE_ClientUsername", request.username),
            ("NewX_AVM-DE_PhoneName", request.phoneName),
            ("NewX_AVM-DE_ClientId", request.clientID),
            ("NewX_AVM-DE_OutGoingNumber", request.outgoingNumber),
            ("NewX_AVM-DE_InComingNumbers", TR064XML.numberList(request.incomingNumbers)),
        ], token: token)
        return values["NewX_AVM-DE_InternalNumber"] ?? ""
    }

    public func startSecondFactor() async throws(TR064Error) -> FritzBoxSecondFactorChallenge {
        let values = try await call(Self.authControl, Self.authService, "SetConfig", [("NewAction", "start")])
        guard let token = values["NewToken"]?.nonEmpty else { throw .invalidResponse("SetConfig: no token") }
        return FritzBoxSecondFactorChallenge(
            token: token,
            methods: FritzBoxConfirmationMethod.parse(values["NewMethods"] ?? ""),
            state: values["NewState"].flatMap(FritzBoxSecondFactorState.init(rawValue:))
        )
    }

    public func secondFactorState(token: String) async throws(TR064Error) -> FritzBoxSecondFactorState {
        let values = try await call(Self.authControl, Self.authService, "GetState", token: token)
        guard let state = values["NewState"].flatMap(FritzBoxSecondFactorState.init(rawValue:)) else {
            throw .invalidResponse("GetState")
        }
        return state
    }

    public func stopSecondFactor(token: String) async {
        _ = try? await call(Self.authControl, Self.authService, "SetConfig", [("NewAction", "stop")], token: token)
    }
}

extension TR064XML {
    /// `<List><Item><Number/><Type/><Index/><Name/></Item>…</List>`; the
    /// client list nests the same items in `X_AVM-DE_InComingNumbers`.
    static func lineNumbers(_ xml: String) -> [FritzBoxLineNumber] {
        guard let tree = XMLTree.parse(Data(xml.utf8)) else { return [] }
        return lineNumbers(items: tree.all(named: "Item"))
    }

    private static func lineNumbers(items: [XMLTree]) -> [FritzBoxLineNumber] {
        items.compactMap { item in
            func text(_ name: String) -> String { item.child(name)?.text.trimmingCharacters(in: .whitespacesAndNewlines) ?? "" }
            let number = text("Number")
            guard !number.isEmpty else { return nil }
            return FritzBoxLineNumber(number: number, type: text("Type"), index: Int(text("Index")) ?? 0, name: text("Name"))
        }
    }

    static func sipClients(_ xml: String) -> [FritzBoxSIPClient] {
        guard let tree = XMLTree.parse(Data(xml.utf8)) else { return [] }
        let items = tree.all(named: "Item").filter { $0.child("X_AVM-DE_ClientIndex") != nil }
        return items.compactMap { item in
            func text(_ name: String) -> String { item.child(name)?.text.trimmingCharacters(in: .whitespacesAndNewlines) ?? "" }
            guard let index = Int(text("X_AVM-DE_ClientIndex")) else { return nil }
            return FritzBoxSIPClient(
                index: index,
                username: text("X_AVM-DE_ClientUsername"),
                phoneName: text("X_AVM-DE_PhoneName"),
                clientID: text("X_AVM-DE_ClientId"),
                outgoingNumber: text("X_AVM-DE_OutGoingNumber"),
                incomingNumbers: lineNumbers(items: item.child("X_AVM-DE_InComingNumbers")?.children.filter { $0.name == "Item" } ?? []),
                internalNumber: text("X_AVM-DE_InternalNumber")
            )
        }
    }

    /// For `NewX_AVM-DE_InComingNumbers`: the format of `X_AVM-DE_GetNumbers`;
    /// empty rings for all numbers (X_VoIP 2.35).
    static func numberList(_ numbers: [FritzBoxLineNumber]) -> String {
        guard !numbers.isEmpty else { return "" }
        var xml = "<List>"
        for number in numbers {
            xml += "<Item><Number>\(escape(number.number))</Number><Type>\(escape(number.type))</Type><Index>\(number.index)</Index><Name>\(escape(number.name))</Name></Item>"
        }
        return xml + "</List>"
    }
}

/// Creates or reuses the IP phone for this iPhone (HPHN-25).
public struct FritzBoxPhoneSetup: Sendable {
    /// X_VoIP variable list: client index 0 … 9.
    public static let maxClients = 10
    /// X_AVM-DE_ClientPasswordAllowedChars, 8 … 64 characters.
    static let passwordAlphabet = Array("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-._")
    static let passwordLength = 32

    public enum Failure: Error, Equatable, Sendable {
        /// All ten IP-phone slots are taken.
        case noFreeSlot
        /// The confirmation at the FRITZ!Box did not happen in time, or
        /// was cancelled.
        case notConfirmed
        case tr064(TR064Error)
    }

    public let service: any FritzBoxVoIPService

    public init(service: any FritzBoxVoIPService) {
        self.service = service
    }

    /// What the box knows already: its numbers, and an IP phone this app
    /// created before (same client ID, or same name).
    public struct Survey: Equatable, Sendable {
        public var numbers: [FritzBoxLineNumber]
        public var clients: [FritzBoxSIPClient]
        public var existing: FritzBoxSIPClient?
        public var hasFreeSlot: Bool
    }

    public func survey(clientID: String?, phoneName: String) async throws(TR064Error) -> Survey {
        let numbers = try await service.numbers()
        let clients = try await service.sipClients()
        let existing = clients.first { clientID != nil && $0.clientID == clientID } ?? clients.first { $0.phoneName == phoneName }
        return Survey(numbers: Self.uniqueNumbers(numbers), clients: clients, existing: existing, hasFreeSlot: Self.freeIndex(in: clients) != nil)
    }

    /// The request for a new client, or for overwriting `existing` (its
    /// password cannot be read back, so it gets a new one).
    public static func request(
        survey: Survey,
        reuse: Bool,
        phoneName: String,
        clientID: String,
        outgoingNumber: String,
        incomingNumbers: [FritzBoxLineNumber],
        password: String = makePassword()
    ) throws(Failure) -> FritzBoxSIPClientRequest {
        let index: Int
        let username: String
        if reuse, let existing = survey.existing {
            index = existing.index
            username = existing.username.isEmpty ? uniqueUsername(taken: survey.clients.map(\.username)) : existing.username
        } else {
            guard let free = freeIndex(in: survey.clients) else { throw .noFreeSlot }
            index = free
            username = uniqueUsername(taken: survey.clients.map(\.username))
        }
        return FritzBoxSIPClientRequest(
            index: index,
            username: username,
            password: password,
            phoneName: phoneName,
            clientID: clientID,
            outgoingNumber: outgoingNumber,
            incomingNumbers: incomingNumbers
        )
    }

    static func freeIndex(in clients: [FritzBoxSIPClient]) -> Int? {
        let used = Set(clients.map(\.index))
        return (0..<maxClients).first { !used.contains($0) }
    }

    /// "housephone", or "housephone2", "housephone3", … if taken.
    static func uniqueUsername(taken: [String]) -> String {
        let taken = Set(taken.map { $0.lowercased() })
        var candidate = "housephone"
        var suffix = 2
        while taken.contains(candidate) {
            candidate = "housephone\(suffix)"
            suffix += 1
        }
        return candidate
    }

    public static func makePassword() -> String {
        var generator = SystemRandomNumberGenerator()
        return String((0..<passwordLength).map { _ in passwordAlphabet.randomElement(using: &generator)! })
    }

    /// The same number can appear once per type; the pickers show it once.
    static func uniqueNumbers(_ numbers: [FritzBoxLineNumber]) -> [FritzBoxLineNumber] {
        var seen = Set<String>()
        return numbers.filter { seen.insert($0.number).inserted }
    }

    /// Writes the client. If the FRITZ!Box wants a confirmation (UPnP 866),
    /// starts it, hands the challenge to `onChallenge` (the UI shows "press
    /// a button" or the code to dial), waits until the user confirmed and
    /// writes again with the token (X_AVM-DE_Auth 6.4, "typical scenario").
    /// Returns the internal number of the IP phone.
    public func apply(
        _ request: FritzBoxSIPClientRequest,
        pollInterval: Duration = .seconds(1),
        timeout: Duration = .seconds(120),
        onChallenge: @Sendable (FritzBoxSecondFactorChallenge) async -> Void
    ) async throws(Failure) -> String {
        do {
            return try await service.setSIPClient(request, token: nil)
        } catch .secondFactorRequired {
            // Confirmation needed; continue below.
        } catch {
            throw .tr064(error)
        }

        let challenge: FritzBoxSecondFactorChallenge
        do {
            challenge = try await service.startSecondFactor()
        } catch {
            throw .tr064(error)
        }
        if let failure = Self.failure(for: challenge.state) { throw failure }
        await onChallenge(challenge)

        let deadline = ContinuousClock.now + timeout
        while true {
            if Task.isCancelled || ContinuousClock.now >= deadline {
                await service.stopSecondFactor(token: challenge.token)
                throw .notConfirmed
            }
            let state: FritzBoxSecondFactorState
            do {
                state = try await service.secondFactorState(token: challenge.token)
            } catch {
                await service.stopSecondFactor(token: challenge.token)
                throw .tr064(error)
            }
            if state == .authenticated { break }
            if let failure = Self.failure(for: state) { throw failure }
            try? await Task.sleep(for: pollInterval)
        }
        do {
            return try await service.setSIPClient(request, token: challenge.token)
        } catch {
            throw .tr064(error)
        }
    }

    /// Terminal states other than "authenticated"; nil while waiting.
    static func failure(for state: FritzBoxSecondFactorState?) -> Failure? {
        switch state {
        case .blocked: .tr064(.secondFactorBlocked)
        case .anotherAuthProcess: .tr064(.secondFactorBusy)
        case .stopped, .failure: .notConfirmed
        case .waitingForAuth, .authenticated, .disabled, nil: nil
        }
    }
}
