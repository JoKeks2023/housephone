import AppIntents
import HousephoneKit

// Intents shared by the app and its widget extension (controls in Control
// Center and on the Action button). They open the app, so `perform` always
// runs in the app process; the extension only needs them to exist.

struct CallFavoriteIntent: AppIntent {
    static var title: LocalizedStringResource { "Favorit anrufen" }
    static var description: IntentDescription { "Ruft einen Favoriten über Housephone an." }
    static var openAppWhenRun: Bool { true }

    @Parameter(title: "Favorit")
    var favorite: FavoriteEntity

    init() {}

    /// For controls: without a favorite (not configured yet) the intent
    /// asks for one.
    init(favorite: FavoriteEntity?) {
        if let favorite { self.favorite = favorite }
    }

    static var parameterSummary: some ParameterSummary {
        Summary("\(\.$favorite) anrufen")
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if HOUSEPHONE_APP
        AppServices.shared.handle(.call(number: favorite.number, name: favorite.name, key: nil), trusted: true)
        #endif
        return .result()
    }
}

struct OpenKeypadIntent: AppIntent {
    static var title: LocalizedStringResource { "Tastenfeld öffnen" }
    static var description: IntentDescription { "Öffnet Housephone mit dem Tastenfeld." }
    static var openAppWhenRun: Bool { true }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if HOUSEPHONE_APP
        AppServices.shared.handle(.keypad, trusted: true)
        #endif
        return .result()
    }
}

struct ShowMissedCallsIntent: AppIntent {
    static var title: LocalizedStringResource { "Verpasste Anrufe zeigen" }
    static var description: IntentDescription { "Öffnet die Anrufliste mit den verpassten Anrufen." }
    static var openAppWhenRun: Bool { true }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if HOUSEPHONE_APP
        AppServices.shared.handle(.recents(missedOnly: true), trusted: true)
        #endif
        return .result()
    }
}
