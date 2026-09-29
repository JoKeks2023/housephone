import HousephoneKit
import SwiftUI

/// Settings section for the Apple Watch: is there one, is Housephone on it,
/// is it paired with the bridge — and one button to pair it.
struct WatchSection: View {
    @Environment(WatchLink.self) private var watch
    @Environment(BridgeConnection.self) private var bridge

    var body: some View {
        if watch.isSupported, watch.isWatchPaired {
            Section {
                statusRow
                action
            } header: {
                Text("Apple Watch")
            } footer: {
                footer
            }
        }
    }

    // MARK: - Status

    @ViewBuilder
    private var statusRow: some View {
        LabeledContent("Status") {
            if !watch.isAppInstalled {
                StatusIndicator(tone: .neutral, label: "Housephone nicht installiert")
            } else if let state = watch.watchState, state.phase == .paired {
                if state.canReceiveCalls {
                    StatusIndicator(tone: .positive, label: "Gekoppelt")
                } else {
                    StatusIndicator(tone: .warning, label: "Gekoppelt, klingelt noch nicht")
                }
            } else {
                StatusIndicator(tone: .neutral, label: "Nicht gekoppelt")
            }
        }
    }

    // MARK: - Action

    @ViewBuilder
    private var action: some View {
        if watch.isAppInstalled {
            switch watch.pairing {
            case .requestingCode:
                progressRow("Code wird angefordert …")
            case .sendingToWatch:
                progressRow("Watch koppelt sich …")
            case .waitingForWatch(let expiresAt):
                VStack(alignment: .leading, spacing: Theme.Space.s2) {
                    progressRow("Öffne Housephone auf deiner Apple Watch")
                    Text("Der Code gilt bis \(expiresAt, style: .time).")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Button("Abbrechen", role: .cancel) { watch.cancelPairing() }
            case .failed(let message):
                Label {
                    Text(message)
                } icon: {
                    Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(Theme.warning)
                }
                .font(.footnote)
                pairButton(title: "Erneut versuchen")
            case .idle:
                pairButton(title: watch.isPairedWithBridge ? "Erneut koppeln" : "Apple Watch koppeln")
            }
        }
    }

    private func pairButton(title: LocalizedStringKey) -> some View {
        Button {
            Task { await watch.pairWatch() }
        } label: {
            Label(title, systemImage: "applewatch.radiowaves.left.and.right")
        }
        .disabled(!bridge.isOnline)
    }

    private func progressRow(_ title: LocalizedStringKey) -> some View {
        HStack(spacing: Theme.Space.s3) {
            ProgressView()
            Text(title)
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var footer: some View {
        if !watch.isAppInstalled {
            Text("Installiere Housephone in der Watch-App auf dem iPhone unter „Verfügbare Apps“.")
        } else if !bridge.isOnline {
            Text("Zum Koppeln muss die Bridge erreichbar sein.")
        } else if watch.isPairedWithBridge {
            Text("Bei Anrufen klingelt auch deine Watch. Du kannst direkt am Handgelenk annehmen und sprechen – auch ohne iPhone in der Nähe.")
        } else {
            Text("Danach klingelt auch deine Watch, und du kannst direkt am Handgelenk telefonieren.")
        }
    }
}
