import HousephoneKit
import SwiftUI

/// The call screen: who, how long, mute, keypad, hang up. The Digital
/// Crown sets the volume.
struct WatchInCallView: View {
    @Environment(WatchCallCenter.self) private var callCenter
    @State private var volume = 0.8
    @State private var showsVolume = false
    @State private var showsKeypad = false
    @State private var hideVolumeTask: Task<Void, Never>?

    var body: some View {
        if let call = callCenter.activeCall {
            content(for: call)
                .focusable()
                .digitalCrownRotation(
                    $volume,
                    from: 0,
                    through: 1,
                    by: 0.05,
                    sensitivity: .low,
                    isContinuous: false,
                    isHapticFeedbackEnabled: true
                )
                .onChange(of: volume) { _, newValue in
                    callCenter.audio.setVolume(Float(newValue))
                    flashVolume()
                }
                .onAppear {
                    callCenter.audio.setVolume(Float(volume))
                }
                .sheet(isPresented: $showsKeypad) {
                    DTMFKeypad()
                }
        }
    }

    private func content(for call: CallSession) -> some View {
        VStack(spacing: WatchTheme.Space.s2) {
            VStack(spacing: 2) {
                Text(title(for: call))
                    .font(.title3.weight(.semibold))
                    .lineLimit(2)
                    .minimumScaleFactor(0.7)
                    .multilineTextAlignment(.center)
                WatchCallStatusLine(call: call, connection: callCenter.connection)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)

            Spacer(minLength: 0)

            if showsVolume {
                volumeIndicator
                    .transition(.opacity)
            }

            if call.direction == .incoming, !call.userAnswered, call.isActive {
                HStack {
                    WatchCallButton(kind: .end, label: "Ablehnen") { callCenter.end() }
                    Spacer()
                    WatchCallButton(kind: .start, label: "Annehmen") { callCenter.answer() }
                }
                .padding(.horizontal, WatchTheme.Space.s2)
            } else {
                HStack(alignment: .center) {
                    controlButton(
                        symbol: callCenter.isMuted ? "mic.slash.fill" : "mic.fill",
                        label: callCenter.isMuted ? "Stumm aus" : "Stumm",
                        isOn: callCenter.isMuted
                    ) {
                        callCenter.setMuted(!callCenter.isMuted)
                    }
                    .disabled(!call.isActive)
                    Spacer()
                    WatchCallButton(kind: .end, label: "Auflegen") { callCenter.end() }
                        .disabled(!call.isActive)
                        .opacity(call.isActive ? 1 : 0.4)
                    Spacer()
                    controlButton(symbol: "circle.grid.3x3.fill", label: "Tastenfeld", isOn: false) {
                        showsKeypad = true
                    }
                    .disabled(call.phase != .connected && call.phase != .earlyMedia)
                }
                .padding(.horizontal, WatchTheme.Space.s1)
            }
        }
        .animation(WatchTheme.Motion.standard, value: showsVolume)
    }

    private func controlButton(symbol: String, label: LocalizedStringKey, isOn: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.body.weight(.semibold))
                .foregroundStyle(isOn ? Color.black : Color.white)
                .frame(width: 40, height: 40)
                .background(Circle().fill(isOn ? Color.white : Color.white.opacity(0.16)))
                .contentTransition(.symbolEffect(.replace))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Text(label))
        .accessibilityAddTraits(isOn ? .isSelected : [])
    }

    private var volumeIndicator: some View {
        HStack(spacing: WatchTheme.Space.s1) {
            Image(systemName: "speaker.wave.2.fill")
                .font(.caption2)
            ProgressView(value: volume)
                .tint(Color.accentColor)
        }
        .foregroundStyle(.secondary)
        .accessibilityElement()
        .accessibilityLabel(Text("Lautstärke"))
        .accessibilityValue(Text(volume, format: .percent.precision(.fractionLength(0))))
    }

    private func flashVolume() {
        showsVolume = true
        hideVolumeTask?.cancel()
        hideVolumeTask = Task {
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled else { return }
            showsVolume = false
        }
    }

    private func title(for call: CallSession) -> String {
        if let name = call.remoteName, !name.isEmpty { return name }
        return call.remoteNumber.isEmpty ? String(localized: "Unbekannt") : call.remoteNumber
    }
}

/// "Klingelt …", "Verbinde …", the running duration, or the outcome.
struct WatchCallStatusLine: View {
    let call: CallSession
    let connection: WatchCallCenter.ConnectionState?

    var body: some View {
        switch call.phase {
        case .connected:
            if connection == .reconnecting {
                Text("Verbindung wird wiederhergestellt …")
            } else if let connectedAt = call.connectedAt {
                TimelineView(.periodic(from: connectedAt, by: 1)) { context in
                    Text(context.date.timeIntervalSince(connectedAt).callDurationText)
                        .monospacedDigit()
                }
            }
        case .ended:
            Text(endedText)
        case .ringing:
            Text(call.direction == .incoming ? "Eingehender Anruf" : "Klingelt …")
        case .earlyMedia:
            Text("Klingelt …")
        case .answering:
            Text("Verbinde …")
        case .waitingForBridge:
            Text(call.direction == .incoming ? "Eingehender Anruf" : "Verbinde …")
        }
    }

    private var endedText: LocalizedStringKey {
        switch call.outcome {
        case .busy: "Besetzt"
        case .answeredElsewhere: "Anderswo angenommen"
        case .failed: "Fehlgeschlagen"
        case .missed: "Verpasst"
        case .declined: "Abgelehnt"
        default: "Beendet"
        }
    }
}

/// Tones during a call, e.g. for voice menus.
struct DTMFKeypad: View {
    @Environment(WatchCallCenter.self) private var callCenter
    @State private var digits = ""

    var body: some View {
        VStack(spacing: WatchTheme.Space.s1) {
            Text(digits.isEmpty ? " " : digits)
                .font(.body.monospacedDigit())
                .lineLimit(1)
                .truncationMode(.head)
            WatchKeyGrid { key in
                digits.append(key)
                callCenter.playDTMF(key)
            } onLongPressZero: {}
        }
        .padding(.horizontal, WatchTheme.Space.s1)
    }
}
