import AppIntents
import HousephoneKit

// Actions for Shortcuts, Spotlight, Siri and the Action button. Each one
// opens the app, which starts the call through CallKit.

/// A favorite as Shortcuts and Siri see it. Read from the App Group
/// snapshot, so widgets can use the same query later.
struct FavoriteEntity: AppEntity {
    static var typeDisplayRepresentation: TypeDisplayRepresentation { "Favorit" }
    static var defaultQuery: FavoriteQuery { FavoriteQuery() }

    let id: UUID
    let name: String
    let number: String
    let label: String?

    init(_ favorite: SharedSnapshot.Favorite) {
        id = favorite.id
        name = favorite.name
        number = favorite.number
        label = favorite.label
    }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(name)", subtitle: "\(label ?? number)")
    }
}

struct FavoriteQuery: EntityStringQuery {
    private var favorites: [FavoriteEntity] {
        (SharedSnapshotStore.appGroup?.load()?.favorites ?? []).map(FavoriteEntity.init)
    }

    func entities(for identifiers: [UUID]) async throws -> [FavoriteEntity] {
        favorites.filter { identifiers.contains($0.id) }
    }

    func entities(matching string: String) async throws -> [FavoriteEntity] {
        NameMatcher.matches(string, in: favorites, name: \.name)
    }

    func suggestedEntities() async throws -> [FavoriteEntity] {
        favorites
    }
}

struct CallFavoriteIntent: AppIntent {
    static var title: LocalizedStringResource { "Favorit anrufen" }
    static var description: IntentDescription { "Ruft einen Favoriten über Housephone an." }
    static var openAppWhenRun: Bool { true }

    @Parameter(title: "Favorit")
    var favorite: FavoriteEntity

    static var parameterSummary: some ParameterSummary {
        Summary("\(\.$favorite) anrufen")
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        AppServices.shared.handle(.call(number: favorite.number, name: favorite.name, key: nil), trusted: true)
        return .result()
    }
}

struct CallNumberIntent: AppIntent {
    static var title: LocalizedStringResource { "Nummer anrufen" }
    static var description: IntentDescription { "Ruft eine Telefonnummer über Housephone an." }
    static var openAppWhenRun: Bool { true }

    @Parameter(title: "Nummer")
    var number: String

    static var parameterSummary: some ParameterSummary {
        Summary("\(\.$number) anrufen")
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        guard let dialable = PhoneNumber.dialable(number) else {
            throw $number.needsValueError("Das ist keine gültige Telefonnummer.")
        }
        AppServices.shared.handle(.call(number: dialable, name: nil, key: nil), trusted: true)
        return .result()
    }
}

struct OpenKeypadIntent: AppIntent {
    static var title: LocalizedStringResource { "Tastenfeld öffnen" }
    static var description: IntentDescription { "Öffnet Housephone mit dem Tastenfeld." }
    static var openAppWhenRun: Bool { true }

    @MainActor
    func perform() async throws -> some IntentResult {
        AppServices.shared.handle(.keypad, trusted: true)
        return .result()
    }
}

struct ShowMissedCallsIntent: AppIntent {
    static var title: LocalizedStringResource { "Verpasste Anrufe zeigen" }
    static var description: IntentDescription { "Öffnet die Anrufliste mit den verpassten Anrufen." }
    static var openAppWhenRun: Bool { true }

    @MainActor
    func perform() async throws -> some IntentResult {
        AppServices.shared.handle(.recents(missedOnly: true), trusted: true)
        return .result()
    }
}

/// Ready without setup in Siri, Spotlight and Shortcuts. Phrases are German
/// (the development language); English is in `AppShortcuts.xcstrings`.
struct HousephoneShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: CallFavoriteIntent(),
            phrases: [
                "\(\.$favorite) mit \(.applicationName) anrufen",
                "Ruf \(\.$favorite) mit \(.applicationName) an",
                "Mit \(.applicationName) \(\.$favorite) anrufen",
                "Favorit mit \(.applicationName) anrufen",
            ],
            shortTitle: "Favorit anrufen",
            systemImageName: "star.fill"
        )
        AppShortcut(
            intent: ShowMissedCallsIntent(),
            phrases: [
                "Verpasste Anrufe in \(.applicationName)",
                "Zeig verpasste Anrufe in \(.applicationName)",
            ],
            shortTitle: "Verpasste Anrufe",
            systemImageName: "phone.arrow.down.left"
        )
        AppShortcut(
            intent: OpenKeypadIntent(),
            phrases: [
                "Öffne das Tastenfeld in \(.applicationName)",
                "\(.applicationName) Tastenfeld",
            ],
            shortTitle: "Tastenfeld",
            systemImageName: "circle.grid.3x3.fill"
        )
    }
}
