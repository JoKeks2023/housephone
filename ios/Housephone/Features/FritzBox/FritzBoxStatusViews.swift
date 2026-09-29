import HousephoneKit
import SwiftUI

/// Where the setup of the FRITZ!Box access is explained.
private let fritzBoxSetupGuide = URL(string: "https://github.com/JoKeks2023/housephone/blob/master/bridge/README.md")!

/// One line above a FRITZ!Box list when the shown data is not current:
/// what is wrong and how old the data is.
struct FritzBoxStaleNotice: View {
    let failure: FritzBoxLoadFailure
    let fetchedAt: Date?

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: Theme.Space.hairline) {
                Text(title)
                    .font(.subheadline.weight(.medium))
                if let fetchedAt {
                    Text("Stand: \(fetchedAt.formatted(.relative(presentation: .named)))")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        } icon: {
            Image(systemName: symbol)
                .foregroundStyle(Theme.warningText)
        }
        .accessibilityElement(children: .combine)
    }

    private var title: LocalizedStringKey {
        switch failure {
        case .unreachable: "Bridge nicht erreichbar"
        case .unavailable: "FRITZ!Box antwortet nicht"
        case .unauthorized: "Kopplung ungültig"
        }
    }

    private var symbol: String {
        switch failure {
        case .unreachable: "wifi.slash"
        case .unavailable: "exclamationmark.triangle.fill"
        case .unauthorized: "link.badge.plus"
        }
    }
}

/// Full-screen state when there is no FRITZ!Box data to show at all.
/// Every case offers a way forward.
struct FritzBoxFailureView: View {
    let failure: FritzBoxLoadFailure
    let retry: () -> Void

    @Environment(AppModel.self) private var appModel

    var body: some View {
        ContentUnavailableView {
            Label(title, systemImage: symbol)
        } description: {
            VStack(spacing: Theme.Space.s2) {
                Text(message)
                if case .unavailable(let detail) = failure, !detail.isEmpty {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        } actions: {
            switch failure {
            case .unauthorized:
                Button("Zu den Einstellungen") {
                    appModel.selectedTab = .settings
                }
                .buttonStyle(.glassProminent)
            case .unavailable:
                Button("Erneut versuchen", action: retry)
                Link("Anleitung öffnen", destination: fritzBoxSetupGuide)
            case .unreachable:
                Button("Erneut versuchen", action: retry)
            }
        }
    }

    private var title: LocalizedStringKey {
        switch failure {
        case .unavailable: "FRITZ!Box nicht verbunden"
        case .unreachable: "Bridge nicht erreichbar"
        case .unauthorized: "Kopplung ungültig"
        }
    }

    private var message: LocalizedStringKey {
        switch failure {
        case .unavailable:
            "Die Bridge hat noch keinen Zugang zu Telefonbuch und Anrufliste der FRITZ!Box. Richte ihn in der Bridge ein."
        case .unreachable:
            "Prüfe die Internetverbindung. Telefonbuch und Anrufliste kommen über deine Bridge."
        case .unauthorized:
            "Die Bridge kennt dieses iPhone nicht mehr. Hebe in den Einstellungen die Kopplung auf und kopple es neu."
        }
    }

    private var symbol: String {
        switch failure {
        case .unavailable: "phone.connection"
        case .unreachable: "wifi.slash"
        case .unauthorized: "link.badge.plus"
        }
    }
}
