import AVFAudio
import HousephoneKit
import SwiftUI

struct SettingsView: View {
    @Environment(BridgeConnection.self) private var bridge
    @Environment(DirectPhone.self) private var direct
    @Environment(WatchLink.self) private var watch
    @Environment(\.openURL) private var openURL
    @Environment(\.scenePhase) private var scenePhase
    @State private var confirmsUnpair = false
    @State private var microphone = AVAudioApplication.shared.recordPermission

    var body: some View {
        NavigationStack {
            Form {
                if direct.isEnabled {
                    DirectSettingsSection()
                    Section("Dieses iPhone") {
                        microphoneRows
                    }
                    WatchSection()
                } else {
                    bridgeSections
                }

                Section("Über") {
                    LabeledContent("Version") {
                        Text(BridgeConnection.appVersion).font(.callout.monospaced())
                    }
                    NavigationLink("Danksagungen") {
                        AcknowledgementsView()
                    }
                }
            }
            .navigationTitle("Einstellungen")
            .confirmationDialog("Kopplung aufheben?", isPresented: $confirmsUnpair, titleVisibility: .visible) {
                Button("Kopplung aufheben", role: .destructive) {
                    // The watch was paired through this iPhone; it follows.
                    watch.unpairWatch()
                    Task { await bridge.unpair() }
                }
            } message: {
                if watch.isPairedWithBridge {
                    Text("Die Zugangsdaten werden von diesem iPhone gelöscht. Deine Apple Watch wird ebenfalls entkoppelt.")
                } else {
                    Text("Die Zugangsdaten werden von diesem iPhone gelöscht.")
                }
            }
            .onAppear {
                microphone = AVAudioApplication.shared.recordPermission
            }
            .onChange(of: scenePhase) { _, phase in
                // Coming back from the Settings app with a changed permission.
                if phase == .active { microphone = AVAudioApplication.shared.recordPermission }
            }
        }
    }

    @ViewBuilder
    private var bridgeSections: some View {
        Section {
            bridgeStatusRow
            if let credentials = bridge.credentials {
                LabeledContent("Name", value: bridge.welcome?.bridgeName ?? credentials.bridgeName)
                LabeledContent("Adresse") {
                    Text(credentials.bridgeURL.host() ?? credentials.bridgeURL.absoluteString)
                        .font(.callout.monospaced())
                        .textSelection(.enabled)
                }
                // The key pinned at pairing, as `fp` in the pairing link.
                VStack(alignment: .leading, spacing: Theme.Space.s1) {
                    Text("Fingerabdruck")
                    Text(credentials.bridgeFingerprint)
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .speechSpellsOutCharacters()
                }
                .accessibilityElement(children: .combine)
            }
            if let version = bridge.welcome?.bridgeVersion {
                LabeledContent("Version") {
                    Text(version).font(.callout.monospaced())
                }
            }
            if let welcome = bridge.welcome {
                LabeledContent("Telefonbuch & Anrufliste") {
                    if welcome.supports(.fritzboxPhonebook) || welcome.supports(.fritzboxHistory) {
                        StatusIndicator(tone: .positive, label: "Verbunden")
                    } else {
                        StatusIndicator(tone: .neutral, label: "Nicht eingerichtet")
                    }
                }
            }
        } header: {
            Text("Bridge")
        } footer: {
            Text("Die Bridge läuft auf deinem Server und ist für die FRITZ!Box ein IP-Telefon.")
        }

        Section {
            LabeledContent("Anrufe im Hintergrund") {
                StatusIndicator(
                    tone: bridge.pushToken == nil ? .warning : .positive,
                    label: bridge.pushToken == nil ? "Noch nicht bereit" : "Bereit"
                )
            }
            microphoneRows
        } header: {
            Text("Dieses iPhone")
        } footer: {
            if bridge.pushToken == nil {
                Text("iOS hat Housephone noch nicht für Anrufe im Hintergrund freigegeben. Das passiert automatisch, sobald die App mit Internet geöffnet ist. Bleibt es dabei, prüfe den Push-Schlüssel (APNs) der Bridge.")
            }
        }

        WatchSection()

        Section {
            Button("Kopplung aufheben", role: .destructive) {
                confirmsUnpair = true
            }
        } footer: {
            if watch.isPairedWithBridge {
                Text("Danach klingeln dieses iPhone und deine Apple Watch nicht mehr. Du kannst beide jederzeit neu koppeln.")
            } else {
                Text("Danach klingelt dieses iPhone nicht mehr. Du kannst es jederzeit neu koppeln.")
            }
        }
    }

    @ViewBuilder
    private var bridgeStatusRow: some View {
        LabeledContent("Status") {
            switch bridge.status {
            case .online(sipRegistered: true):
                StatusIndicator(tone: .positive, label: "Verbunden")
            case .online(sipRegistered: false):
                StatusIndicator(tone: .warning, label: "FRITZ!Box nicht angemeldet")
            case .connecting:
                StatusIndicator(tone: .neutral, label: "Verbinde …", isBusy: true)
            case .offline:
                switch bridge.problem {
                case .untrustedBridge:
                    StatusIndicator(tone: .negative, label: "Bridge nicht vertrauenswürdig")
                case .clockSkew:
                    StatusIndicator(tone: .warning, label: "Uhrzeit des iPhones prüfen")
                case nil:
                    StatusIndicator(tone: .negative, label: "Nicht erreichbar")
                }
            case .rejected:
                StatusIndicator(tone: .negative, label: "Kopplung ungültig")
            case .unpaired:
                StatusIndicator(tone: .neutral, label: "Nicht gekoppelt")
            }
        }
    }

    /// Status in one row, the fix as its own action below: a status value
    /// stays short and never doubles as a button.
    @ViewBuilder
    private var microphoneRows: some View {
        switch microphone {
        case .granted:
            LabeledContent("Mikrofon") {
                StatusIndicator(tone: .positive, label: "Erlaubt")
            }
        case .denied:
            LabeledContent("Mikrofon") {
                StatusIndicator(tone: .negative, label: "Nicht erlaubt")
            }
            Button("In Einstellungen erlauben") {
                if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
            }
        default:
            LabeledContent("Mikrofon") {
                StatusIndicator(tone: .neutral, label: "Noch nicht gefragt")
            }
            Button("Mikrofon erlauben") {
                Task {
                    _ = await AVAudioApplication.requestRecordPermission()
                    microphone = AVAudioApplication.shared.recordPermission
                }
            }
        }
    }
}
