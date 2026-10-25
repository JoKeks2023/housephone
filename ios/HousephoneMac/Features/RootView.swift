import HousephoneKit
import SwiftUI

struct RootView: View {
    @Environment(MacBridge.self) private var bridge

    var body: some View {
        if bridge.isPaired {
            StatusView()
        } else {
            PairingView()
        }
    }
}

/// Pairing with the link the bridge shows (`housephone-bridge pair`).
private struct PairingView: View {
    @Environment(MacBridge.self) private var bridge
    @State private var text = ""
    @State private var isPairing = false
    @State private var failure: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Housephone")
                .font(.largeTitle.bold())
            Text("Füge den Kopplungslink deiner Bridge ein. Du bekommst ihn mit `housephone-bridge pair`.")
                .foregroundStyle(.secondary)

            TextField("housephone://pair?…", text: $text)
                .textFieldStyle(.roundedBorder)
                .onSubmit(pair)
                .disabled(isPairing)

            HStack {
                Button("Koppeln", action: pair)
                    .keyboardShortcut(.defaultAction)
                    .disabled(text.isEmpty || isPairing)
                if isPairing { ProgressView().controlSize(.small) }
            }

            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.red)
            }
            Spacer()
        }
        .padding(24)
    }

    private func pair() {
        failure = nil
        let link: PairingLink
        do {
            link = try PairingLink(string: text)
        } catch {
            failure = "Das ist kein gültiger Kopplungslink."
            return
        }
        isPairing = true
        Task {
            defer { isPairing = false }
            do {
                try await bridge.pair(with: link)
                text = ""
            } catch {
                failure = "Kopplung fehlgeschlagen: \(error.localizedDescription)"
            }
        }
    }
}

private struct StatusView: View {
    @Environment(MacBridge.self) private var bridge
    @State private var confirmsUnpair = false

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(bridge.credentials?.bridgeName ?? "Bridge")
                .font(.largeTitle.bold())

            Label(statusText, systemImage: statusSymbol)
                .foregroundStyle(statusColor)

            if let problem = bridge.problem {
                Text(problemText(problem))
                    .foregroundStyle(.secondary)
            }
            if bridge.isOnHomeNetwork {
                Label("Direkt im Heimnetz", systemImage: "house")
                    .foregroundStyle(.secondary)
            }
            if let fingerprint = bridge.credentials?.bridgeFingerprint {
                LabeledContent("Kennung der Bridge") {
                    Text(fingerprint).font(.caption.monospaced()).textSelection(.enabled)
                }
            }

            Spacer()
            Button("Kopplung aufheben…", role: .destructive) { confirmsUnpair = true }
        }
        .padding(24)
        .confirmationDialog("Kopplung aufheben?", isPresented: $confirmsUnpair, titleVisibility: .visible) {
            Button("Kopplung aufheben", role: .destructive) {
                Task { await bridge.unpair() }
            }
        } message: {
            Text("Die Zugangsdaten werden von diesem Mac gelöscht.")
        }
    }

    private var statusText: String {
        switch bridge.status {
        case .unpaired: "Nicht gekoppelt"
        case .connecting: "Verbinde…"
        case .online(let sipRegistered): sipRegistered ? "Verbunden, FRITZ!Box angemeldet" : "Verbunden, FRITZ!Box nicht erreichbar"
        case .offline: "Offline"
        case .rejected: "Von der Bridge entfernt. Koppeln erneut."
        }
    }

    private var statusSymbol: String {
        switch bridge.status {
        case .online(true): "checkmark.circle.fill"
        case .online(false), .connecting: "exclamationmark.circle"
        case .offline, .rejected, .unpaired: "xmark.circle.fill"
        }
    }

    private var statusColor: Color {
        switch bridge.status {
        case .online(true): .green
        case .online(false), .connecting: .orange
        case .offline, .rejected, .unpaired: .red
        }
    }

    private func problemText(_ problem: MacBridge.Problem) -> String {
        switch problem {
        case .untrustedBridge: "Die Bridge konnte ihre Identität nicht belegen. Die Verbindung wurde nicht aufgebaut."
        case .clockSkew: "Die Uhr dieses Macs weicht zu stark von der der Bridge ab."
        }
    }
}
