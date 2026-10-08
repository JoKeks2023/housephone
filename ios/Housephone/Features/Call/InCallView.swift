import AVKit
import Combine
import HousephoneKit
import SwiftUI

/// The call screen: caller, status, controls. CallKit shows its own UI on
/// the lock screen; this is what the user sees inside the app.
struct InCallView: View {
    @Environment(CallCenter.self) private var callCenter
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(AppModel.self) private var appModel
    @State private var showsKeypad = false
    @State private var dtmfDigits = ""
    /// The caller's contact photo, full size, for the avatar and the
    /// background.
    @State private var photo: Data?
    @Namespace private var glass

    var body: some View {
        ZStack {
            CallBackground(photo: photo, tint: AvatarTint.forName(callerName))
            if let call = callCenter.activeCall {
                GeometryReader { proxy in
                    content(for: call, height: proxy.size.height)
                }
            }
        }
        .preferredColorScheme(.dark)
        .task(id: callCenter.activeCall?.remoteNumber) {
            guard let number = callCenter.activeCall?.remoteNumber, !number.isEmpty else {
                photo = nil
                return
            }
            let loaded = await contacts.image(for: number)
            withMotion(Theme.Motion.standard) { photo = loaded }
        }
        .motion(Theme.Motion.standard, value: showsKeypad)
        .onChange(of: callCenter.activeCall?.phase) { _, phase in
            guard let phase, let call = callCenter.activeCall else { return }
            if phase == .ended { showsKeypad = false }
            AccessibilityNotification.Announcement(CallStatusLine.announcement(for: call)).post()
        }
    }

    private var callerName: String? {
        guard let call = callCenter.activeCall else { return nil }
        return contacts.name(for: call.remoteNumber) ?? call.remoteName
    }

    // MARK: - Layout

    /// Fixed parts in keypad mode: top bar 44 + 8, compact header ~92,
    /// top padding 16, two spacers 2 × 24, digits line 42, grid row
    /// spacing 3 × 16, end button 76, bottom padding 24. Four key rows
    /// scale; see the T-0007 report for iPhone SE / 16 / 16 Pro.
    private static let keypadFixedHeight: CGFloat = 398

    private static func keySize(for height: CGFloat) -> CGFloat {
        min(74, max(52, (height - keypadFixedHeight) / 4))
    }

    private func content(for call: CallSession, height: CGFloat) -> some View {
        let name = contacts.name(for: call.remoteNumber) ?? call.remoteName
        return VStack(spacing: 0) {
            topBar(call: call)

            header(call: call, name: name)
                .padding(.top, showsKeypad ? Theme.Space.s4 : Theme.Space.s8)

            Spacer(minLength: Theme.Space.s6)

            GlassGroup(spacing: Theme.Space.s4) {
                if showsKeypad {
                    VStack(spacing: Theme.Space.s4) {
                        Text(dtmfDigits.isEmpty ? " " : dtmfDigits)
                            .font(.title.monospacedDigit())
                            .foregroundStyle(.white)
                            .lineLimit(1)
                            .truncationMode(.head)
                            .accessibilityLabel(dtmfDigits.isEmpty ? Text("Noch keine Tastentöne") : Text.spokenNumber(dtmfDigits))
                        KeypadGrid(style: .glass, keySize: Self.keySize(for: height), glassNamespace: glass) { digit in
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
            }

            Spacer(minLength: Theme.Space.s6)

            bottomBar(call: call)
                .padding(.bottom, showsKeypad ? Theme.Space.s6 : Theme.Space.s12)
        }
        .padding(.horizontal, Theme.Space.s6)
    }

    // MARK: - Top bar

    @ViewBuilder
    private func topBar(call: CallSession) -> some View {
        HStack {
            if AppModel.canMinimizeCall, call.isActive, !showsIncomingActions(call) {
                Button {
                    appModel.isCallMinimized = true
                } label: {
                    Image(systemName: "chevron.down")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.white)
                        .frame(width: 44, height: 44)
                        .glassSurface(in: .circle, interactive: true)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(Text("Minimieren"))
                .accessibilityHint(Text("Zeigt die App; der Anruf läuft weiter"))
            }
            Spacer()
        }
        .frame(height: 44)
        .padding(.top, Theme.Space.s2)
    }

    // MARK: - Header

    private func header(call: CallSession, name: String?) -> some View {
        VStack(spacing: showsKeypad ? Theme.Space.s1 : Theme.Space.s3) {
            if !showsKeypad {
                AvatarView(name: name, imageData: photo, size: 128)
                    .shadow(color: .black.opacity(0.3), radius: 16, y: 6)
                    .transition(.opacity.combined(with: .scale(scale: 0.96)))
            }
            Text(name ?? (call.remoteNumber.isEmpty ? String(localized: "Unbekannt") : call.remoteNumber))
                .font(showsKeypad ? .title3.weight(.semibold) : .title.weight(.semibold))
                .foregroundStyle(.white)
                .multilineTextAlignment(.center)
                .lineLimit(2)
                .minimumScaleFactor(0.7)
            if name != nil, !call.remoteNumber.isEmpty {
                Text(call.remoteNumber)
                    .font(.callout)
                    .monospacedDigit()
                    .foregroundStyle(.white.opacity(0.6))
                    .accessibilityLabel(Text.spokenNumber(call.remoteNumber))
            }
            CallStatusLine(call: call, mediaState: callCenter.mediaState)
                .padding(.top, Theme.Space.s1)
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    // MARK: - Controls

    private func controls(call: CallSession) -> some View {
        let keypadAvailable = call.phase == .connected || call.phase == .earlyMedia
        return HStack(spacing: Theme.Space.s8) {
            CallControlButton(
                symbol: callCenter.isMuted ? "mic.slash" : "mic",
                label: "Stumm",
                isOn: callCenter.isMuted,
                morph: GlassMorph(id: "mute", namespace: glass)
            ) {
                callCenter.setMuted(!callCenter.isMuted)
            }
            CallControlButton(
                symbol: "circle.grid.3x3",
                label: "Tastenfeld",
                morph: GlassMorph(id: KeypadGrid.centerGlassID, namespace: glass)
            ) {
                withMotion(Theme.Motion.standard) { showsKeypad = true }
            }
            .disabled(!keypadAvailable)
            .opacity(keypadAvailable ? 1 : 0.4)
            AudioOutputButton(morph: GlassMorph(id: "audio", namespace: glass))
        }
        .disabled(!call.isActive)
        .opacity(call.isActive ? 1 : 0.4)
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
                    // The button already says it; don't read it twice.
                    Text("Ablehnen").font(.footnote.weight(.medium)).foregroundStyle(.white.opacity(0.85))
                        .accessibilityHidden(true)
                }
                .frame(maxWidth: .infinity)
                VStack(spacing: Theme.Space.s2) {
                    CallActionButton(kind: .start, label: "Annehmen") { callCenter.answer() }
                    Text("Annehmen").font(.footnote.weight(.medium)).foregroundStyle(.white.opacity(0.85))
                        .accessibilityHidden(true)
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
                            withMotion(Theme.Motion.standard) { showsKeypad = false }
                        }
                        .font(.body.weight(.medium))
                        .foregroundStyle(.white)
                        .frame(minHeight: 44)
                    }
                }
            }
        }
    }
}

/// "Verbinde …", "Klingelt …", the running duration, or how it ended.
/// Each state replaces the last with a soft blur instead of a jump.
struct CallStatusLine: View {
    let call: CallSession
    let mediaState: MediaEngine.ConnectionState?

    private var isReconnecting: Bool {
        call.phase == .connected && (mediaState == .interrupted || mediaState == .failed)
    }

    var body: some View {
        Group {
            switch call.phase {
            case .connected:
                if isReconnecting {
                    Text("Verbindung wird wiederhergestellt …")
                } else if let connectedAt = call.connectedAt {
                    Text(timerInterval: connectedAt...Date.distantFuture, countsDown: false)
                        .monospacedDigit()
                }
            default:
                Text(Self.text(for: call))
            }
        }
        .font(.headline.weight(.regular))
        .foregroundStyle(.white.opacity(0.7))
        .id(StatusKey(phase: call.phase, reconnecting: isReconnecting))
        .transition(.blurReplace)
        .motion(Theme.Motion.standard, value: StatusKey(phase: call.phase, reconnecting: isReconnecting))
        .sensoryFeedback(.impact(weight: .light), trigger: call.phase) { _, new in new == .connected }
    }

    private struct StatusKey: Hashable {
        let phase: CallPhase
        let reconnecting: Bool
    }

    static func text(for call: CallSession) -> LocalizedStringResource {
        switch call.phase {
        case .connected: "Verbunden"
        case .ended: call.outcome.map { $0 == .answered ? "Beendet" : $0.label } ?? "Beendet"
        case .waitingForBridge: call.direction == .incoming ? "Eingehender Anruf" : "Verbinde …"
        case .ringing: call.direction == .incoming ? "Eingehender Anruf" : "Klingelt …"
        case .earlyMedia: "Klingelt …"
        case .answering: "Verbinde …"
        }
    }

    /// What VoiceOver announces when the call changes state.
    static func announcement(for call: CallSession) -> String {
        String(localized: text(for: call))
    }
}

/// Like the Phone app: the caller's photo, blurred and darkened, fills
/// the screen. Without a photo, true black with a soft glow in the
/// caller's identity color (or the accent for unknown numbers).
private struct CallBackground: View {
    let photo: Data?
    let tint: AvatarTint?

    @Environment(\.accessibilityReduceTransparency) private var reduceTransparency

    var body: some View {
        ZStack {
            Color.black
            if let image = photo.flatMap(UIImage.init(data:)), !reduceTransparency {
                // Overlay on a clear view: the filled image must not grow
                // the layout beyond the screen.
                Color.clear
                    .overlay {
                        Image(uiImage: image)
                            .resizable()
                            .scaledToFill()
                            .blur(radius: 60, opaque: true)
                    }
                    .clipped()
                    .overlay(Color.black.opacity(0.45))
                    .transition(.opacity)
            } else {
                RadialGradient(
                    colors: [(tint?.color ?? Color.accentColor).opacity(0.32), .clear],
                    center: .init(x: 0.5, y: -0.05),
                    startRadius: 0,
                    endRadius: 560
                )
                LinearGradient(
                    colors: [.clear, (tint?.color ?? Color.accentColor).opacity(0.10)],
                    startPoint: .center,
                    endPoint: .bottom
                )
            }
        }
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }
}

/// Speaker on/off in one tap when the iPhone is the only output, like the
/// Phone app; the system route picker once headphones, a car or
/// Bluetooth are connected.
private struct AudioOutputButton: View {
    let morph: GlassMorph
    @State private var route = AudioRoute.current()

    var body: some View {
        Group {
            if route.hasExternalOutput {
                routePicker
            } else {
                CallControlButton(symbol: "speaker.wave.2", label: "Lautsprecher", isOn: route.isSpeaker, morph: morph) {
                    AudioRoute.setSpeaker(!route.isSpeaker)
                    route = AudioRoute.current()
                }
                .accessibilityInputLabels([Text("Lautsprecher"), Text("Audio")])
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: AVAudioSession.routeChangeNotification).receive(on: RunLoop.main)) { _ in
            route = AudioRoute.current()
        }
    }

    private var routePicker: some View {
        VStack(spacing: Theme.Space.s2) {
            RoutePicker()
                .frame(width: 72, height: 72)
                .overlay {
                    Image(systemName: route.symbol)
                        .font(.title2.weight(.medium))
                        .foregroundStyle(route.isSpeaker ? Color.black : Color.white)
                        .contentTransition(.symbolEffect(.replace))
                        .frame(width: 72, height: 72)
                        .background {
                            Circle().fill(Color.white).opacity(route.isSpeaker ? 1 : 0)
                        }
                        .allowsHitTesting(false)
                }
                .glassSurface(in: .circle, interactive: true)
                .glassMorph(morph)
            Text("Audio")
                .font(.footnote.weight(.medium))
                .foregroundStyle(.white.opacity(0.85))
        }
        .motion(Theme.Motion.snappy, value: route)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(Text("Audioausgabe"))
        .accessibilityValue(Text(verbatim: route.spokenName))
        .accessibilityAddTraits(.isButton)
        .accessibilityInputLabels([Text("Audio"), Text("Lautsprecher"), Text("Audioausgabe")])
    }
}

private struct AudioRoute: Equatable {
    let isSpeaker: Bool
    let hasExternalOutput: Bool
    let symbol: String
    let spokenName: String

    static func current() -> AudioRoute {
        let session = AVAudioSession.sharedInstance()
        let output = session.currentRoute.outputs.first
        let external: Set<AVAudioSession.Port> = [.bluetoothHFP, .bluetoothA2DP, .bluetoothLE, .headphones, .usbAudio, .carAudio, .airPlay]
        let hasExternal = session.currentRoute.outputs.contains { external.contains($0.portType) }
            || (session.availableInputs ?? []).contains { [.bluetoothHFP, .headsetMic, .carAudio, .usbAudio].contains($0.portType) }

        switch output?.portType {
        case .builtInSpeaker:
            return AudioRoute(isSpeaker: true, hasExternalOutput: hasExternal, symbol: "speaker.wave.2.fill", spokenName: String(localized: "Lautsprecher"))
        case .bluetoothHFP, .bluetoothA2DP, .bluetoothLE:
            let name = output?.portName ?? ""
            let symbol = name.localizedCaseInsensitiveContains("AirPods") ? "airpods" : "headphones"
            return AudioRoute(isSpeaker: false, hasExternalOutput: true, symbol: symbol, spokenName: String(localized: "Bluetooth"))
        case .headphones, .usbAudio:
            return AudioRoute(isSpeaker: false, hasExternalOutput: true, symbol: "headphones", spokenName: String(localized: "Kopfhörer"))
        case .carAudio:
            return AudioRoute(isSpeaker: false, hasExternalOutput: true, symbol: "car.fill", spokenName: String(localized: "Auto"))
        default:
            return AudioRoute(isSpeaker: false, hasExternalOutput: hasExternal, symbol: "iphone", spokenName: String(localized: "iPhone"))
        }
    }

    /// Only used without external outputs; CallKit owns the session, the
    /// override just moves the output between receiver and speaker.
    static func setSpeaker(_ on: Bool) {
        try? AVAudioSession.sharedInstance().overrideOutputAudioPort(on ? .speaker : .none)
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
