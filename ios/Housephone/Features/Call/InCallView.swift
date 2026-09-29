import AVKit
import Combine
import HousephoneKit
import SwiftUI

/// The call screen: caller, status, controls. CallKit shows its own UI on
/// the lock screen; this is what the user sees inside the app.
struct InCallView: View {
    @Environment(CallCenter.self) private var callCenter
    @Environment(ContactsDirectory.self) private var contacts
    @State private var showsKeypad = false
    @State private var dtmfDigits = ""

    var body: some View {
        ZStack {
            CallBackground()
            if let call = callCenter.activeCall {
                content(for: call)
            }
        }
        .preferredColorScheme(.dark)
        .motion(Theme.Motion.standard, value: showsKeypad)
    }

    private func content(for call: CallSession) -> some View {
        let name = contacts.name(for: call.remoteNumber) ?? call.remoteName
        return VStack(spacing: 0) {
            header(call: call, name: name)
                .padding(.top, Theme.Space.s12)

            Spacer(minLength: Theme.Space.s6)

            if showsKeypad {
                VStack(spacing: Theme.Space.s4) {
                    Text(dtmfDigits.isEmpty ? " " : dtmfDigits)
                        .font(.title.monospacedDigit())
                        .foregroundStyle(.white)
                        .lineLimit(1)
                        .truncationMode(.head)
                    KeypadGrid(style: .glass, keySize: 74) { digit in
                        dtmfDigits.append(digit)
                        callCenter.playDTMF(digit)
                    }
                }
                .transition(.opacity.combined(with: .scale(scale: 0.97)))
            } else if showsIncomingActions(call) {
                EmptyView()
            } else {
                controls(call: call)
                    .transition(.opacity.combined(with: .scale(scale: 0.97)))
            }

            Spacer(minLength: Theme.Space.s6)

            bottomBar(call: call)
                .padding(.bottom, Theme.Space.s12)
        }
        .padding(.horizontal, Theme.Space.s6)
    }

    // MARK: - Header

    private func header(call: CallSession, name: String?) -> some View {
        VStack(spacing: Theme.Space.s3) {
            AvatarView(name: name, size: 88)
            Text(name ?? (call.remoteNumber.isEmpty ? String(localized: "Unbekannt") : call.remoteNumber))
                .font(.system(size: 30, weight: .semibold))
                .tracking(-0.4)
                .foregroundStyle(.white)
                .multilineTextAlignment(.center)
                .lineLimit(2)
                .minimumScaleFactor(0.7)
            if name != nil, !call.remoteNumber.isEmpty {
                Text(call.remoteNumber)
                    .font(.callout)
                    .monospacedDigit()
                    .foregroundStyle(.white.opacity(0.6))
            }
            CallStatusLine(call: call, mediaState: callCenter.mediaState)
                .padding(.top, Theme.Space.s1)
        }
        .accessibilityElement(children: .combine)
    }

    // MARK: - Controls

    private func controls(call: CallSession) -> some View {
        GlassEffectContainer(spacing: Theme.Space.s8) {
            HStack(spacing: Theme.Space.s8) {
                CallControlButton(symbol: callCenter.isMuted ? "mic.slash" : "mic", label: "Stumm", isOn: callCenter.isMuted) {
                    callCenter.setMuted(!callCenter.isMuted)
                }
                CallControlButton(symbol: "circle.grid.3x3", label: "Tastenfeld") {
                    showsKeypad = true
                }
                .disabled(call.phase != .connected && call.phase != .earlyMedia)
                .opacity(call.phase == .connected || call.phase == .earlyMedia ? 1 : 0.4)
                AudioRouteButton()
            }
        }
        .disabled(!call.isActive)
    }

    private func showsIncomingActions(_ call: CallSession) -> Bool {
        call.direction == .incoming && !call.userAnswered && call.isActive
    }

    @ViewBuilder
    private func bottomBar(call: CallSession) -> some View {
        if showsIncomingActions(call) {
            HStack {
                VStack(spacing: Theme.Space.s2) {
                    CallActionButton(kind: .end, label: "Ablehnen") { callCenter.end() }
                    Text("Ablehnen").font(.footnote.weight(.medium)).foregroundStyle(.white.opacity(0.85))
                }
                .frame(maxWidth: .infinity)
                VStack(spacing: Theme.Space.s2) {
                    CallActionButton(kind: .start, label: "Annehmen") { callCenter.answer() }
                    Text("Annehmen").font(.footnote.weight(.medium)).foregroundStyle(.white.opacity(0.85))
                }
                .frame(maxWidth: .infinity)
            }
        } else {
            ZStack {
                CallActionButton(kind: .end, label: "Auflegen", isEnabled: call.isActive) {
                    callCenter.end()
                }
                if showsKeypad {
                    HStack {
                        Spacer()
                        Button("Ausblenden") {
                            showsKeypad = false
                        }
                        .font(.body.weight(.medium))
                        .foregroundStyle(.white)
                    }
                }
            }
        }
    }
}

/// "Verbinde …", "Klingelt …", the running duration, or how it ended.
private struct CallStatusLine: View {
    let call: CallSession
    let mediaState: MediaEngine.ConnectionState?

    var body: some View {
        Group {
            switch call.phase {
            case .connected:
                if mediaState == .interrupted || mediaState == .failed {
                    Text("Verbindung wird wiederhergestellt …")
                } else if let connectedAt = call.connectedAt {
                    TimelineView(.periodic(from: connectedAt, by: 1)) { context in
                        Text(context.date.timeIntervalSince(connectedAt).callDurationText)
                            .monospacedDigit()
                    }
                }
            case .ended:
                Text(call.outcome.map { $0 == .answered ? "Beendet" : $0.label } ?? "Beendet")
            case .waitingForBridge:
                Text(call.direction == .incoming ? "Eingehender Anruf" : "Verbinde …")
            case .ringing:
                Text(call.direction == .incoming ? "Eingehender Anruf" : "Klingelt …")
            case .earlyMedia:
                Text("Klingelt …")
            case .answering:
                Text("Verbinde …")
            }
        }
        .font(.headline.weight(.regular))
        .foregroundStyle(.white.opacity(0.7))
        .contentTransition(.opacity)
    }
}

/// True black with a soft accent glow from the top: the call screen is
/// the app's signature surface.
private struct CallBackground: View {
    var body: some View {
        ZStack {
            Color.black
            RadialGradient(
                colors: [Color.accentColor.opacity(0.28), .clear],
                center: .init(x: 0.5, y: -0.05),
                startRadius: 0,
                endRadius: 520
            )
        }
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }
}

/// Audio route (iPhone, speaker, AirPods …) via the system route picker,
/// styled like the other call controls.
private struct AudioRouteButton: View {
    @State private var routeSymbol = AudioRoute.currentSymbol

    var body: some View {
        VStack(spacing: Theme.Space.s2) {
            RoutePicker()
                .frame(width: 72, height: 72)
                .overlay {
                    Image(systemName: routeSymbol)
                        .font(.title2.weight(.medium))
                        .foregroundStyle(.white)
                        .allowsHitTesting(false)
                }
                .glassEffect(.regular.interactive(), in: .circle)
            Text("Audio")
                .font(.footnote.weight(.medium))
                .foregroundStyle(.white.opacity(0.85))
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(Text("Audioausgabe"))
        .onReceive(NotificationCenter.default.publisher(for: AVAudioSession.routeChangeNotification)) { _ in
            routeSymbol = AudioRoute.currentSymbol
        }
    }
}

private enum AudioRoute {
    static var currentSymbol: String {
        let output = AVAudioSession.sharedInstance().currentRoute.outputs.first?.portType
        switch output {
        case .builtInSpeaker: return "speaker.wave.2.fill"
        case .bluetoothHFP, .bluetoothA2DP, .bluetoothLE: return "airpods"
        case .headphones, .usbAudio: return "headphones"
        case .carAudio: return "car.fill"
        default: return "speaker.wave.2"
        }
    }
}

private struct RoutePicker: UIViewRepresentable {
    func makeUIView(context: Context) -> AVRoutePickerView {
        let picker = AVRoutePickerView()
        picker.prioritizesVideoDevices = false
        // The glyph is drawn by SwiftUI on top; hide the system one.
        picker.tintColor = .clear
        picker.activeTintColor = .clear
        return picker
    }

    func updateUIView(_ uiView: AVRoutePickerView, context: Context) {}
}
