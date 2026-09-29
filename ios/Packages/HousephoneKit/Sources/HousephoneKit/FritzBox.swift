import Foundation

// Phonebook and call list of the FRITZ!Box, as the bridge serves them over
// HTTPS (signaling v1.2, `GET /v1/phonebook` and `GET /v1/history`).
//
// Open string enums (`FritzBoxNumberType`, `FritzBoxCall.Direction` …) keep
// values a newer bridge might send, so decoding never fails on them.

/// Extra functions a bridge offers, from `welcome.features`.
public struct BridgeFeature: RawRepresentable, Codable, Sendable, Hashable {
    public var rawValue: String

    public init(rawValue: String) {
        self.rawValue = rawValue
    }

    public init(from decoder: any Decoder) throws {
        rawValue = try decoder.singleValueContainer().decode(String.self)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }

    /// `GET /v1/phonebook` works.
    public static let fritzboxPhonebook = Self(rawValue: "fritzbox.phonebook")
    /// `GET /v1/history` works.
    public static let fritzboxHistory = Self(rawValue: "fritzbox.history")
}

// MARK: - Phonebook

public struct FritzBoxPhonebook: Codable, Sendable, Equatable {
    public var updatedAt: Date
    public var contacts: [FritzBoxContact]

    public init(updatedAt: Date, contacts: [FritzBoxContact]) {
        self.updatedAt = updatedAt
        self.contacts = contacts
    }
}

public struct FritzBoxContact: Codable, Sendable, Equatable, Hashable, Identifiable {
    /// `<phonebook id>-<uniqueid>`, stable while the contact exists.
    public var id: String
    public var name: String
    /// The FRITZ!Box category "VIP".
    public var favorite: Bool
    /// Name of the FRITZ!Box phonebook the contact is in.
    public var phonebook: String?
    public var numbers: [FritzBoxNumber]

    public init(id: String, name: String, favorite: Bool, phonebook: String?, numbers: [FritzBoxNumber]) {
        self.id = id
        self.name = name
        self.favorite = favorite
        self.phonebook = phonebook
        self.numbers = numbers
    }

    /// The number to call on a single tap: the preferred one, else the first.
    public var primaryNumber: FritzBoxNumber? {
        numbers.first(where: \.preferred) ?? numbers.first
    }
}

public struct FritzBoxNumber: Codable, Sendable, Equatable, Hashable {
    /// As stored in the FRITZ!Box, only `+0-9*#`, e.g. `030123456` or `**620`.
    public var number: String
    public var type: FritzBoxNumberType
    public var preferred: Bool

    public init(number: String, type: FritzBoxNumberType, preferred: Bool) {
        self.number = number
        self.type = type
        self.preferred = preferred
    }
}

public struct FritzBoxNumberType: RawRepresentable, Codable, Sendable, Hashable {
    public var rawValue: String

    public init(rawValue: String) {
        self.rawValue = rawValue
    }

    public init(from decoder: any Decoder) throws {
        rawValue = try decoder.singleValueContainer().decode(String.self)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }

    public static let home = Self(rawValue: "home")
    public static let mobile = Self(rawValue: "mobile")
    public static let work = Self(rawValue: "work")
    public static let faxWork = Self(rawValue: "fax_work")
    /// Internal FRITZ!Box numbers such as `**620`.
    public static let intern = Self(rawValue: "intern")
    public static let memo = Self(rawValue: "memo")
    public static let other = Self(rawValue: "other")
}

// MARK: - Call list

public struct FritzBoxCallList: Codable, Sendable, Equatable {
    public var updatedAt: Date
    /// Newest first.
    public var calls: [FritzBoxCall]

    public init(updatedAt: Date, calls: [FritzBoxCall]) {
        self.updatedAt = updatedAt
        self.calls = calls
    }

    /// Missed incoming calls since `date`, newest first.
    public func missedCalls(since date: Date) -> [FritzBoxCall] {
        calls.filter { $0.isMissed && $0.startedAt >= date }
    }
}

/// One entry of the line's call list: every call the FRITZ!Box saw, also
/// those at other phones or the answering machine.
public struct FritzBoxCall: Codable, Sendable, Equatable, Hashable, Identifiable {
    public struct Direction: RawRepresentable, Codable, Sendable, Hashable {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public init(from decoder: any Decoder) throws { rawValue = try decoder.singleValueContainer().decode(String.self) }
        public func encode(to encoder: any Encoder) throws {
            var container = encoder.singleValueContainer()
            try container.encode(rawValue)
        }

        public static let incoming = Self(rawValue: "incoming")
        public static let outgoing = Self(rawValue: "outgoing")
    }

    public struct Result: RawRepresentable, Codable, Sendable, Hashable {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public init(from decoder: any Decoder) throws { rawValue = try decoder.singleValueContainer().decode(String.self) }
        public func encode(to encoder: any Encoder) throws {
            var container = encoder.singleValueContainer()
            try container.encode(rawValue)
        }

        public static let answered = Self(rawValue: "answered")
        public static let missed = Self(rawValue: "missed")
        /// Refused by the FRITZ!Box, e.g. by call barring.
        public static let rejected = Self(rawValue: "rejected")
        /// Still running.
        public static let active = Self(rawValue: "active")
    }

    public struct AnsweredBy: RawRepresentable, Codable, Sendable, Hashable {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public init(from decoder: any Decoder) throws { rawValue = try decoder.singleValueContainer().decode(String.self) }
        public func encode(to encoder: any Encoder) throws {
            var container = encoder.singleValueContainer()
            try container.encode(rawValue)
        }

        public static let phone = Self(rawValue: "phone")
        public static let answeringMachine = Self(rawValue: "answering_machine")
    }

    public var id: String
    public var direction: Direction
    public var result: Result
    /// The other party. Empty when withheld.
    public var number: String
    /// The other party's name as the FRITZ!Box knows it.
    public var name: String?
    /// The FRITZ!Box device that took or made the call, e.g. "Mobilteil 1".
    public var device: String?
    public var answeredBy: AnsweredBy?
    public var startedAt: Date
    /// Whole minutes (the FRITZ!Box rounds up) times 60. 0 when not connected.
    public var durationSeconds: Int

    public init(
        id: String,
        direction: Direction,
        result: Result,
        number: String,
        name: String? = nil,
        device: String? = nil,
        answeredBy: AnsweredBy? = nil,
        startedAt: Date,
        durationSeconds: Int
    ) {
        self.id = id
        self.direction = direction
        self.result = result
        self.number = number
        self.name = name
        self.device = device
        self.answeredBy = answeredBy
        self.startedAt = startedAt
        self.durationSeconds = durationSeconds
    }

    public var isMissed: Bool { direction == .incoming && result == .missed }
    public var isAnsweringMachine: Bool { answeredBy == .answeringMachine }
}

// MARK: - Caller names

/// Number → name lookup over the FRITZ!Box phonebook, for caller ID.
///
/// Numbers are compared in national form (`+49 30 1`, `0049301` and `0301`
/// match). A number that belongs to two different names gives no name —
/// better none than a wrong one. Internal numbers (`**620`) are skipped.
public struct PhonebookNameIndex: Sendable, Equatable {
    private var names: [String: String?] = [:]

    public init() {}

    public init(_ phonebook: FritzBoxPhonebook?, countryCode: String = "49") {
        guard let phonebook else { return }
        for contact in phonebook.contacts {
            let name = contact.name.trimmingCharacters(in: .whitespaces)
            guard !name.isEmpty else { continue }
            for entry in contact.numbers {
                guard let key = PhoneNumber.matchKey(entry.number, countryCode: countryCode) else { continue }
                switch names[key] {
                case .none:
                    names[key] = .some(name)
                case .some(.some(let existing)) where existing != name:
                    names[key] = .some(nil)
                default:
                    break
                }
            }
        }
    }

    public func name(for number: String, countryCode: String = "49") -> String? {
        guard let key = PhoneNumber.matchKey(number, countryCode: countryCode) else { return nil }
        return names[key] ?? nil
    }

    public var isEmpty: Bool { names.isEmpty }
}
