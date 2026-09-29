import AVFAudio
import HousephoneKit
import SwiftUI

struct SettingsView: View {
    @Environment(BridgeConnection.self) private var bridge
    @Environment(WatchLink.self) private var watch
    @Environment(\.openURL) private var openURL
    @State private var confirmsUnpair = false
    @State private var microphone = AVAudioApplication.shared.recordPermission

    var body: some View {
        NavigationStack {
            Form {
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

                Section("Dieses iPhone") {
                    LabeledContent("Anrufe im Hintergrund") {
                        StatusIndicator(
                            tone: bridge.pushToken == nil ? .warning : .positive,
                            label: bridge.pushToken == nil ? "Noch nicht bereit" : "Bereit"
                        )
                    }
                    microphoneRow
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

    @ViewBuilder
    private var microphoneRow: some View {
        switch microphone {
        case .granted:
            LabeledContent("Mikrofon") {
                StatusIndicator(tone: .positive, label: "Erlaubt")
            }
        case .denied:
            Button {
                if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
            } label: {
                LabeledContent("Mikrofon") {
                    StatusIndicator(tone: .negative, label: "Verweigert – in Einstellungen ändern")
                }
            }
        default:
            Button {
                Task {
                    _ = await AVAudioApplication.requestRecordPermission()
                    microphone = AVAudioApplication.shared.recordPermission
                }
            } label: {
                LabeledContent("Mikrofon") {
                    Text("Erlauben").foregroundStyle(Color.accentColor)
                }
            }
        }
    }
}
