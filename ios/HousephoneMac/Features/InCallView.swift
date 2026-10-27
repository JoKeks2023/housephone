import HousephoneKit
import SwiftUI

/// The running call: who, how long, mute and hang up; answer and decline
/// while an incoming call rings.
struct InCallView: View {
    @Environment(MacCallCenter.self) private var calls
    let call: CallSession

    var body: some View {
        VStack(spacing: 24) {
            Spacer()
            VStack(spacing: 6) {
                Text(title)
                    .font(.title.bold())
                    .multilineTextAlignment(.center)
                if call.remoteName != nil, !call.remoteNumber.isEmpty {
                    Text(call.remoteNumber).foregroundStyle(.secondary)
                }
                statusLine
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
            Spacer()
            buttons
        }
        .padding(32)
        .frame(maxWidth: .infinity)
    }

    private var title: String {
        call.remoteName ?? (call.remoteNumber.isEmpty ? "Unbekannt" : call.remoteNumber)
    }

    @ViewBuilder
    private var statusLine: some View {
        switch call.phase {
        case .connected:
            if let connectedAt = call.connectedAt {
                Text(timerInterval: connectedAt...Date.distantFuture, countsDown: false)
            }
        case .ended:
            Text("Beendet")
        case .ringing where call.direction == .incoming:
            Text("Eingehender Anruf")
        case .ringing:
            Text("Es klingelt…")
        case .answering:
            Text("Verbinde…")
        case .waitingForBridge, .earlyMedia:
            Text(call.direction == .incoming ? "Eingehender Anruf" : "Verbinde…")
        }
    }

    @ViewBuilder
    private var buttons: some View {
        if call.direction == .incoming, call.phase == .ringing || call.phase == .waitingForBridge, !call.userAnswered {
            HStack(spacing: 24) {
                roundButton("Ablehnen", systemImage: "phone.down.fill", tint: .red) { calls.end() }
                roundButton("Annehmen", systemImage: "phone.fill", tint: .green) { calls.answer() }
                    .keyboardShortcut(.defaultAction)
            }
        } else {
            HStack(spacing: 24) {
                roundButton(calls.isMuted ? "Ton an" : "Stumm", systemImage: calls.isMuted ? "mic.slash.fill" : "mic.fill", tint: calls.isMuted ? .orange : .gray) {
                    calls.setMuted(!calls.isMuted)
                }
                roundButton("Auflegen", systemImage: "phone.down.fill", tint: .red) { calls.end() }
                    .keyboardShortcut(.defaultAction)
            }
            .disabled(call.phase == .ended)
        }
    }

    private func roundButton(_ title: String, systemImage: String, tint: Color, action: @escaping () -> Void) -> some View {
        VStack(spacing: 6) {
            Button(action: action) {
                Image(systemName: systemImage)
                    .font(.title2)
                    .frame(width: 56, height: 56)
                    .background(tint, in: Circle())
                    .foregroundStyle(.white)
            }
            .buttonStyle(.plain)
            Text(title).font(.caption)
        }
        .accessibilityElement(children: .combine)
    }
}
