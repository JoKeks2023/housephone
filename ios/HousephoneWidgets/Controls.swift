import AppIntents
import SwiftUI
import WidgetKit

// Control Center, Lock Screen and Action button (iOS 18). The intents open
// the app, which starts the call.

@available(iOS 18.0, *)
struct ChooseFavoriteIntent: ControlConfigurationIntent {
    static var title: LocalizedStringResource { "Favorit wählen" }

    @Parameter(title: "Favorit")
    var favorite: FavoriteEntity?

    func perform() async throws -> some IntentResult {
        .result()
    }
}

@available(iOS 18.0, *)
struct CallFavoriteControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        AppIntentControlConfiguration(kind: "CallFavoriteControl", intent: ChooseFavoriteIntent.self) { configuration in
            ControlWidgetButton(action: CallFavoriteIntent(favorite: configuration.favorite)) {
                Label(configuration.favorite?.name ?? String(localized: "Favorit"), systemImage: "phone.fill")
            }
        }
        .displayName("Favorit anrufen")
        .description("Ruft einen Favoriten über Housephone an.")
        .promptsForUserConfiguration()
    }
}

@available(iOS 18.0, *)
struct OpenKeypadControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "OpenKeypadControl") {
            ControlWidgetButton(action: OpenKeypadIntent()) {
                Label("Tastenfeld", systemImage: "circle.grid.3x3.fill")
            }
        }
        .displayName("Tastenfeld öffnen")
        .description("Öffnet Housephone mit dem Tastenfeld.")
    }
}
