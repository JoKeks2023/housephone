import AVFAudio
import HousephoneKit
import SwiftUI

/// Confirms and runs the pairing for a scanned, pasted or opened link.
struct PairingView: View {
    let link: PairingLink

    @Environment(BridgeConnection.self) private var bridge
    @Environment(\.dismiss) private var dismiss

    private enum Phase: Equatable {
        case confirm
        case pairing
        case microphone
        case failed(PairingFailure)

        var isFailed: Bool {
            if case .failed = self { true } else { false }
        }
    }

    @State private var phase: Phase = .confirm
    @State private var successTrigger = 0
    @State private var errorTrigger = 0

    private var bridgeName: String {
        link.bridgeName ?? link.bridgeURL.host() ?? link.bridgeURL.absoluteString
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: Theme.Space.s6) {
                Spacer(minLength: 0)
                AppGlyph(size: 56)
                VStack(spacing: Theme.Space.s2) {
                    title
                        .font(.title2.weight(.semibold))
                        .multilineTextAlignment(.center)
                    detail
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .motion(Theme.Motion.standard, value: phase)
                Spacer(minLength: 0)
                actions
            }
            .padding(Theme.Space.s6)
            .toolbar {
                if phase == .microphone {
                    // Already paired: nothing left to cancel.
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Später") { dismiss() }
                    }
                } else if phase != .pairing {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Abbrechen", role: .cancel) { dismiss() }
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .interactiveDismissDisabled(phase == .pairing)
        .sensoryFeedback(.success, trigger: successTrigger)
        .sensoryFeedback(.error, trigger: errorTrigger)
    }

    @ViewBuilder
    private var title: some View {
        switch phase {
        case .confirm, .pairing: Text("Mit „\(bridgeName)“ koppeln?")
        case .microphone: Text("Gekoppelt")
        case .failed: Text("Kopplung fehlgeschlagen")
        }
    }

    @ViewBuilder
    private var detail: some View {
        switch phase {
        case .confirm, .pairing:
            VStack(spacing: Theme.Space.s2) {
                if bridge.isPaired {
                    Text("Die bisherige Kopplung wird ersetzt.")
                }
                Text(link.bridgeURL.host() ?? "")
                    .font(.callout.monospaced())
                // Compare with the code on the server.
                Text("Code \(Text(link.groupedCode).monospaced().speechSpellsOutCharacters())")
                    .font(.callout)
                if !link.isEncrypted {
                    Label("Unverschlüsselte Verbindung – nur fürs Heimnetz gedacht", systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(Theme.warning)
                }
            }
        case .microphone:
            Text("Erlaube noch das Mikrofon, damit man dich beim Telefonieren hört.")
        case .failed(let failure):
            Text(failure.message)
        }
    }

    @ViewBuilder
    private var actions: some View {
        switch phase {
        case .confirm, .pairing, .failed:
            Button {
                Task { await pair() }
            } label: {
                ZStack {
                    // Keeps the button's size while the spinner shows.
                    Text(phase.isFailed ? "Erneut versuchen" : "Koppeln")
                        .opacity(phase == .pairing ? 0 : 1)
                    if phase == .pairing {
                        ProgressView()
                            .accessibilityLabel(Text("Kopplung läuft"))
                    }
                }
                .font(.body.weight(.semibold))
                .frame(maxWidth: .infinity)
                .padding(.vertical, Theme.Space.s2)
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
            .disabled(phase == .pairing)
        case .microphone:
            Button {
                Task {
                    _ = await AVAudioApplication.requestRecordPermission()
                    dismiss()
                }
            } label: {
                Text("Mikrofon erlauben")
                    .font(.body.weight(.semibold))
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, Theme.Space.s2)
            }
            .buttonStyle(.glassProminent)
            .controlSize(.large)
        }
    }

    private func pair() async {
        phase = .pairing
        do {
            try await bridge.pair(with: link)
            successTrigger += 1
            if AVAudioApplication.shared.recordPermission == .undetermined {
                phase = .microphone
            } else {
                dismiss()
            }
        } catch {
            phase = .failed(PairingFailure(error))
            errorTrigger += 1
        }
    }
}

private enum PairingFailure: Equatable {
    case codeInvalid
    case rateLimited
    case unreachable
    /// The private listener from the link didn't answer: not at home.
    case homeNetworkRequired
    /// The bridge doesn't hold the key from the QR code's fingerprint.
    case bridgeMismatch
    case untrusted
    case clockSkew
    case storage
    case other(String)

    init(_ error: any Error) {
        switch error {
        case HP2Error.bridgeIdentityMismatch:
            self = .bridgeMismatch
        case BridgeHTTPError.homeNetworkRequired:
            self = .homeNetworkRequired
        case SignalingClientError.bridge(let payload) where payload.code == .homeNetworkRequired:
            self = .homeNetworkRequired
        case is HP2Error, SignalingClientError.untrustedBridge:
            self = .untrusted
        case SignalingClientError.clockSkew:
            self = .clockSkew
        case SignalingClientError.bridge(let payload) where payload.code == .pairingInvalid:
            self = .codeInvalid
        case SignalingClientError.bridge(let payload) where payload.code == .pairingRateLimited:
            self = .rateLimited
        case SignalingClientError.bridge(let payload):
            self = .other(payload.message)
        case is SignalingClientError, is URLError, is WebSocketTransportError:
            self = .unreachable
        case is KeychainError:
            self = .storage
        default:
            self = .other(error.localizedDescription)
        }
    }

    var message: LocalizedStringResource {
        switch self {
        case .codeInvalid: "Der Code ist abgelaufen oder wurde schon benutzt. Erzeuge auf dem Server mit „housephone-bridge pair“ einen neuen."
        case .rateLimited: "Zu viele Versuche. Warte eine Minute und versuche es dann erneut."
        case .unreachable: "Die Bridge ist nicht erreichbar. Prüfe die Adresse und deine Internetverbindung."
        case .homeNetworkRequired: "Zum Koppeln ins Heim-WLAN oder Tailscale"
        case .bridgeMismatch: "Diese Bridge ist nicht die aus dem QR-Code. Die Kopplung wurde abgebrochen."
        case .untrusted: "Bridge nicht vertrauenswürdig. Die Kopplung wurde abgebrochen."
        case .clockSkew: "Uhrzeit des iPhones prüfen: Sie weicht zu stark von der Bridge ab."
        case .storage: "Die Zugangsdaten konnten nicht sicher gespeichert werden."
        case .other(let message): "Die Bridge meldet: \(message)"
        }
    }
}
