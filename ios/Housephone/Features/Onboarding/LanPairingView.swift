import AVFAudio
import HousephoneKit
import SwiftUI

/// Where a LAN pairing goes: a bridge found via Bonjour, or an address
/// typed in (Tailscale, where Bonjour does not reach).
enum LanPairingTarget: Identifiable, Equatable {
    case discovered(DiscoveredBridge)
    case address(URL)

    var id: String {
        switch self {
        case .discovered(let bridge): "bonjour|" + bridge.id
        case .address(let url): "address|" + url.absoluteString
        }
    }
}

/// Pairs in the home network without a QR code (ADR-0007): shows the
/// confirmation code and waits until it is approved on the bridge.
struct LanPairingView: View {
    let target: LanPairingTarget

    @Environment(BridgeConnection.self) private var bridge
    @Environment(\.dismiss) private var dismiss

    private enum Phase: Equatable {
        case confirm
        case connecting
        case waiting(sas: String, expiresAt: Date)
        case denied
        case expired
        case microphone
        case failed(PairingFailure)
    }

    @State private var phase: Phase = .confirm
    @State private var session: LanPairingSession?
    @State private var task: Task<Void, Never>?
    @State private var successTrigger = 0
    @State private var errorTrigger = 0

    private var bridgeName: String {
        switch target {
        case .discovered(let found): found.name
        case .address(let url): session?.bridgeName ?? url.host() ?? url.absoluteString
        }
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: Theme.Space.s6) {
                Spacer(minLength: 0)
                AppGlyph(size: 56)
                VStack(spacing: Theme.Space.s3) {
                    title
                        .font(.title2.weight(.semibold))
                        .multilineTextAlignment(.center)
                    content
                }
                .motion(Theme.Motion.standard, value: phase)
                Spacer(minLength: 0)
                actions
            }
            .padding(Theme.Space.s6)
            .toolbar {
                if phase != .microphone {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Abbrechen", role: .cancel) { cancel() }
                    }
                } else {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Später") { dismiss() }
                    }
                }
            }
        }
        .presentationDetents([.large])
        .interactiveDismissDisabled(phase == .connecting || isWaiting)
        .sensoryFeedback(.success, trigger: successTrigger)
        .sensoryFeedback(.error, trigger: errorTrigger)
        .onDisappear { cancel(dismissing: false) }
    }

    private var isWaiting: Bool {
        if case .waiting = phase { true } else { false }
    }

    @ViewBuilder
    private var title: some View {
        switch phase {
        case .confirm, .connecting: Text("Bridge „\(bridgeName)“ gefunden – koppeln?")
        case .waiting: Text("Code vergleichen")
        case .denied: Text("Kopplung abgelehnt")
        case .expired: Text("Zeit abgelaufen")
        case .microphone: Text("Gekoppelt")
        case .failed: Text("Kopplung fehlgeschlagen")
        }
    }

    @ViewBuilder
    private var content: some View {
        switch phase {
        case .confirm, .connecting:
            VStack(spacing: Theme.Space.s2) {
                if bridge.isPaired {
                    Text("Die bisherige Kopplung wird ersetzt.")
                }
                Text("Housephone zeigt gleich einen Code. Vergleiche ihn mit der Anzeige auf deiner Bridge und gib die Kopplung dort frei.")
            }
            .font(.body)
            .foregroundStyle(.secondary)
            .multilineTextAlignment(.center)
            .fixedSize(horizontal: false, vertical: true)
        case .waiting(let sas, let expiresAt):
            SASCodeView(sas: sas, expiresAt: expiresAt)
        case .denied:
            secondary("Auf der Bridge wurde die Kopplung abgelehnt. Wenn das ein Versehen war, versuche es erneut.")
        case .expired:
            secondary("Die Freigabe auf der Bridge kam nicht rechtzeitig. Versuche es erneut und gib die Kopplung innerhalb von zwei Minuten frei.")
        case .microphone:
            secondary("Erlaube noch das Mikrofon, damit man dich beim Telefonieren hört.")
        case .failed(let failure):
            secondary(failure.message)
        }
    }

    private func secondary(_ text: LocalizedStringResource) -> some View {
        Text(text)
            .font(.body)
            .foregroundStyle(.secondary)
            .multilineTextAlignment(.center)
            .fixedSize(horizontal: false, vertical: true)
    }

    @ViewBuilder
    private var actions: some View {
        switch phase {
        case .confirm, .connecting, .denied, .expired, .failed:
            Button {
                start()
            } label: {
                ZStack {
                    Text(phase == .confirm || phase == .connecting ? "Koppeln" : "Erneut versuchen")
                        .opacity(phase == .connecting ? 0 : 1)
                    if phase == .connecting {
                        ProgressView()
                            .accessibilityLabel(Text("Verbindung zur Bridge wird aufgebaut"))
                    }
                }
                .font(.body.weight(.semibold))
                .frame(maxWidth: .infinity)
                .padding(.vertical, Theme.Space.s2)
            }
            .glassButtonStyle(prominent: true)
            .controlSize(.large)
            .disabled(phase == .connecting)
        case .waiting:
            Label("Warte auf Freigabe …", systemImage: "hourglass")
                .font(.callout)
                .foregroundStyle(.secondary)
                .symbolEffect(.pulse, options: .repeating)
                .padding(.vertical, Theme.Space.s3)
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
            .glassButtonStyle(prominent: true)
            .controlSize(.large)
        }
    }

    private func start() {
        task?.cancel()
        phase = .connecting
        task = Task { await run() }
    }

    private func run() async {
        do {
            let url: URL
            switch target {
            case .discovered(let found): url = try await BridgeBrowser.resolve(found)
            case .address(let address): url = address
            }
            let session = try await bridge.startLanPairing(lanURL: url)
            self.session = session
            phase = .waiting(sas: session.sas, expiresAt: session.expiresAt)
            let outcome = try await bridge.finishLanPairing(session)
            self.session = nil
            switch outcome {
            case .approved:
                successTrigger += 1
                if AVAudioApplication.shared.recordPermission == .undetermined {
                    phase = .microphone
                } else {
                    dismiss()
                }
            case .denied:
                phase = .denied
                errorTrigger += 1
            case .expired:
                phase = .expired
                errorTrigger += 1
            }
        } catch is CancellationError {
            return
        } catch {
            session = nil
            phase = .failed(PairingFailure(error))
            errorTrigger += 1
        }
    }

    private func cancel(dismissing: Bool = true) {
        task?.cancel()
        task = nil
        if let session {
            bridge.cancelLanPairing(session)
            self.session = nil
        }
        if dismissing { dismiss() }
    }
}

/// The six digits, large and spelled out for VoiceOver, with the time left.
private struct SASCodeView: View {
    let sas: String
    let expiresAt: Date

    var body: some View {
        VStack(spacing: Theme.Space.s4) {
            Text(HP2.groupedSAS(sas))
                .font(.system(size: 48, weight: .semibold, design: .monospaced))
                .monospacedDigit()
                .padding(.horizontal, Theme.Space.s6)
                .padding(.vertical, Theme.Space.s4)
                .glassSurface(in: .rect(cornerRadius: Theme.Radius.xl))
                .accessibilityLabel(Text("Code \(Text(sas).speechSpellsOutCharacters())"))
                .textSelection(.disabled)
            Text("Vergleiche diesen Code mit der Anzeige auf deiner Bridge. Gib die Kopplung nur frei, wenn beide gleich sind.")
                .font(.body)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            Text("Freigeben in der TUI (Taste 3), mit „housephone-bridge devices approve“ oder im Home-Assistant-Dashboard.")
                .font(.footnote)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            Text("Gültig noch \(Text(timerInterval: Date.now...max(expiresAt, .now), countsDown: true))")
                .font(.footnote.monospacedDigit())
                .foregroundStyle(.secondary)
        }
    }
}

/// Typed address of the bridge's home network listener, for Tailscale
/// (Bonjour does not cross into the tailnet).
struct LanAddressEntryView: View {
    var onSubmit: (URL) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var host = ""
    @State private var port = "8081"

    private var url: URL? {
        let trimmed = host.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, let portNumber = Int(port), (1...65535).contains(portNumber),
              !trimmed.contains("/"), !trimmed.contains(" ")
        else { return nil }
        let hostPart = trimmed.contains(":") && !trimmed.hasPrefix("[") ? "[\(trimmed)]" : trimmed
        return URL(string: "ws://\(hostPart):\(portNumber)/v1/ws")
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("IP-Adresse der Bridge", text: $host, prompt: Text(verbatim: "100.101.102.103"))
                        .keyboardType(.numbersAndPunctuation)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    TextField("Port", text: $port, prompt: Text(verbatim: "8081"))
                        .keyboardType(.numberPad)
                } footer: {
                    Text("Für Tailscale: die Tailscale-IP des Servers. Im Heim-WLAN findet Housephone die Bridge von selbst.")
                }
            }
            .navigationTitle("Adresse eingeben")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Abbrechen", role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Weiter") {
                        if let url {
                            dismiss()
                            onSubmit(url)
                        }
                    }
                    .disabled(url == nil)
                }
            }
        }
        .presentationDetents([.medium])
    }
}
