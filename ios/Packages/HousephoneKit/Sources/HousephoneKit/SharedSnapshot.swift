import Foundation

/// What the app's extensions (widgets, controls, CarPlay lists) show while
/// the app itself may not be running. The app writes it into the App Group
/// whenever favorites or recent calls change; extensions only read it.
public struct SharedSnapshot: Codable, Sendable, Equatable {
    public struct Favorite: Codable, Sendable, Equatable, Hashable, Identifiable {
        public var id: UUID
        public var name: String
        public var number: String
        /// "Mobil", "Privat" …
        public var label: String?

        public init(id: UUID, name: String, number: String, label: String? = nil) {
            self.id = id
            self.name = name
            self.number = number
            self.label = label
        }
    }

    public struct RecentCall: Codable, Sendable, Equatable, Hashable, Identifiable {
        /// The app's call ID or the FRITZ!Box's entry ID.
        public var id: String
        public var number: String
        public var name: String?
        public var direction: CallDirection
        public var outcome: CallOutcome
        public var date: Date

        public init(id: String, number: String, name: String?, direction: CallDirection, outcome: CallOutcome, date: Date) {
            self.id = id
            self.number = number
            self.name = name
            self.direction = direction
            self.outcome = outcome
            self.date = date
        }

        public var isMissed: Bool { direction == .incoming && outcome == .missed }
    }

    public var favorites: [Favorite]
    /// Newest first, at most `recentCallsLimit`.
    public var recentCalls: [RecentCall]
    /// When the recents were last looked at in the app: missed calls after
    /// this count as new.
    public var recentsSeenAt: Date
    /// Paired with a bridge or set up without one. Whether the bridge is
    /// reachable right now is not known while the app is suspended, so
    /// extensions don't show it.
    public var isSetUp: Bool

    public static let recentCallsLimit = 20

    public init(favorites: [Favorite] = [], recentCalls: [RecentCall] = [], recentsSeenAt: Date = .distantPast, isSetUp: Bool = false) {
        self.favorites = favorites
        self.recentCalls = Array(recentCalls.prefix(Self.recentCallsLimit))
        self.recentsSeenAt = recentsSeenAt
        self.isSetUp = isSetUp
    }

    public static let empty = SharedSnapshot()

    /// Missed calls since the recents were last looked at.
    public var newMissedCalls: [RecentCall] {
        recentCalls.filter { $0.isMissed && $0.date > recentsSeenAt }
    }
}

/// The App Group shared by the iPhone app and its extensions:
/// `group.<prefix>.housephone`, derived from the bundle ID like the VoIP
/// topic, so a foreign bundle prefix needs no extra setting.
public enum AppGroup {
    /// `com.example.housephone.widgets` → `group.com.example.housephone`.
    /// `nil` if the bundle ID has no `housephone` component.
    public static func identifier(forBundleIdentifier bundleID: String) -> String? {
        let components = bundleID.split(separator: ".", omittingEmptySubsequences: false)
        guard let index = components.lastIndex(of: "housephone"), index > 0 else { return nil }
        return "group." + components[...index].joined(separator: ".")
    }

    public static var current: String? {
        Bundle.main.bundleIdentifier.flatMap(identifier(forBundleIdentifier:))
    }

    /// The group's directory, or `nil` without the App Group entitlement
    /// (e.g. an unsigned build).
    public static var containerURL: URL? {
        guard let current else { return nil }
        return FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: current)
    }
}

/// Reads and writes the snapshot as JSON in a directory, normally the App
/// Group container.
public struct SharedSnapshotStore: Sendable {
    public let fileURL: URL

    public init(directory: URL) {
        fileURL = directory.appending(path: "snapshot.json")
    }

    /// The store in the App Group, or `nil` without the entitlement.
    public static var appGroup: SharedSnapshotStore? {
        AppGroup.containerURL.map(SharedSnapshotStore.init(directory:))
    }

    public func load() -> SharedSnapshot? {
        guard let data = try? Data(contentsOf: fileURL) else { return nil }
        return try? Self.decoder.decode(SharedSnapshot.self, from: data)
    }

    /// Writes atomically, so an extension never reads half a file.
    /// Returns `false` if nothing changed.
    @discardableResult
    public func save(_ snapshot: SharedSnapshot) throws -> Bool {
        if load() == snapshot { return false }
        let data = try Self.encoder.encode(snapshot)
        try FileManager.default.createDirectory(at: fileURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        try data.write(to: fileURL, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        return true
    }

    private static let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = .sortedKeys
        return encoder
    }()

    private static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()
}
