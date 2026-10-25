import AppIntents
import HousephoneKit

// Actions for Shortcuts, Spotlight, Siri and the Action button that only
// the app offers. The ones widgets and controls use as well are in
// `Shared/SystemIntents.swift`.

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
