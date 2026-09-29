import SwiftUI

/// Settings of the mode without bridge: status, the account, editing and
/// switching the mode off.
struct DirectSettingsSection: View {
    @Environment(DirectPhone.self) private var direct
    @State private var editing = false
    @State private var confirmsDisable = false

    var body: some View {
        Section {
            DirectStatusRow()
            if let configuration = direct.configuration {
                LabeledContent("FRITZ!Box") {
                    Text(configuration.registrar).font(.callout.monospaced())
                }
                LabeledContent("IP-Telefon") {
                    Text(configuration.sipUsername).font(.callout.monospaced())
                }
                LabeledContent("Heim-WLAN") {
                    if configuration.homeSSID.isEmpty {
                        Text("Nicht angegeben").foregroundStyle(.secondary)
                    } else {
                        Text(configuration.homeSSID)
                    }
                }
                LabeledContent("Telefonbuch & Anrufliste") {
                    if configuration.usesTR064 {
                        StatusIndicator(tone: .positive, label: "Direkt per TR-064")
                    } else {
                        StatusIndicator(tone: .neutral, label: "Nicht eingerichtet")
                    }
                }
            }
            Button("Bearbeiten") { editing = true }
        } header: {
            Text("Direkt mit FRITZ!Box")
        } footer: {
            Text("Nur im Heim-WLAN verfügbar. Housephone ist dann ein IP-Telefon deiner FRITZ!Box, ohne Bridge dazwischen.")
        }
        .sheet(isPresented: $editing) {
            if let configuration = direct.configuration {
                DirectSetupView(configuration: configuration)
            }
        }

        Section {
            DirectLimitsList()
        } header: {
            Text("Grenzen ohne Bridge")
        }

        Section {
            Button("Direktmodus beenden", role: .destructive) {
                confirmsDisable = true
            }
        } footer: {
            Text("Danach kannst du Housephone mit einer Bridge koppeln oder neu einrichten.")
        }
        .confirmationDialog("Direktmodus beenden?", isPresented: $confirmsDisable, titleVisibility: .visible) {
            Button("Direktmodus beenden", role: .destructive) { direct.disable() }
        } message: {
            Text("Housephone meldet sich von der FRITZ!Box ab und löscht die Zugangsdaten von diesem iPhone.")
        }
    }
}

struct DirectStatusRow: View {
    @Environment(DirectPhone.self) private var direct

    var body: some View {
        LabeledContent("Status") {
            switch direct.status {
            case .ready:
                StatusIndicator(tone: .positive, label: "Angemeldet")
            case .connecting:
                StatusIndicator(tone: .neutral, label: "Verbinde …", isBusy: true)
            case .notAtHome:
                StatusIndicator(tone: .warning, label: "Unterwegs nicht erreichbar")
            case .wrongPassword:
                StatusIndicator(tone: .negative, label: "Anmeldung abgelehnt")
            case .rejected(let code):
                StatusIndicator(tone: .negative, label: "FRITZ!Box-Fehler \(code)")
            case .off:
                StatusIndicator(tone: .neutral, label: "Aus")
            }
        }
    }
}
