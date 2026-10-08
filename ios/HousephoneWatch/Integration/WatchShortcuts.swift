import AppIntents

/// "Ruf <Favorit> mit Housephone an" on the watch. The iPhone's in-app Siri
/// handler (any name) has no counterpart on watchOS without an Intents
/// extension, so the watch offers its favorites as App Shortcuts.
struct WatchShortcuts: AppShortcutsProvider {
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
    }
}
