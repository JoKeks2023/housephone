import HousephoneKit
import SwiftData
import SwiftUI

struct KeypadView: View {
    @Environment(AppModel.self) private var appModel
    @Environment(CallCenter.self) private var callCenter
    @Environment(ContactsDirectory.self) private var contacts

    @Query(
        filter: #Predicate<CallRecord> { $0.directionRaw == "outgoing" },
        sort: \CallRecord.date,
        order: .reverse
    )
    private var outgoingCalls: [CallRecord]

    @State private var clearTrigger = 0

    var body: some View {
        @Bindable var appModel = appModel
        let number = appModel.keypadNumber

        VStack(spacing: 0) {
            BridgeStatusBanner()
                .padding(.horizontal, Theme.Space.s4)
                .padding(.top, Theme.Space.s2)

            Spacer(minLength: Theme.Space.s6)

            NumberDisplay(number: number, contactName: contacts.name(for: number)) { pasted in
                appModel.keypadNumber = pasted
            }
            .padding(.horizontal, Theme.Space.s6)

            Spacer(minLength: Theme.Space.s6)

            KeypadGrid { key in
                appModel.keypadNumber.append(key)
            }

            HStack {
                Color.clear.frame(width: 78, height: 78)
                    .frame(maxWidth: .infinity)

                CallActionButton(kind: .start, label: "Anrufen") {
                    call()
                }
                .frame(maxWidth: .infinity)

                DeleteKey(isVisible: !number.isEmpty) {
                    appModel.keypadNumber.removeLast()
                } onClear: {
                    appModel.keypadNumber = ""
                    clearTrigger += 1
                }
                .frame(maxWidth: .infinity)
            }
            .padding(.horizontal, Theme.Space.s8)
            .padding(.top, Theme.Space.s4)
            .padding(.bottom, Theme.Space.s8)
        }
        .sensoryFeedback(.impact(weight: .medium), trigger: clearTrigger)
    }

    private func call() {
        let number = appModel.keypadNumber
        guard !number.isEmpty else {
            // Like the Phone app: an empty call button recalls the last number.
            if let last = outgoingCalls.first?.number { appModel.keypadNumber = last }
            return
        }
        Task {
            if await callCenter.startCall(to: number, name: contacts.name(for: number)) {
                appModel.keypadNumber = ""
            }
        }
    }
}

private struct NumberDisplay: View {
    let number: String
    let contactName: String?
    let onPaste: (String) -> Void

    var body: some View {
        VStack(spacing: Theme.Space.s2) {
            Text(number.isEmpty ? " " : number)
                .font(.system(size: 38, weight: .regular))
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.45)
                .truncationMode(.head)
                .contentTransition(.numericText())
                .animation(Theme.Motion.snappy, value: number)
                .accessibilityLabel(number.isEmpty ? Text("Keine Nummer eingegeben") : Text(number))

            ZStack {
                if let contactName {
                    Text(contactName)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Color.accentColor)
                        .transition(.opacity)
                } else if number.isEmpty {
                    PasteButton(payloadType: String.self) { strings in
                        guard let text = strings.first, let dialable = PhoneNumber.dialable(text) else { return }
                        onPaste(dialable)
                    }
                    .labelStyle(.titleAndIcon)
                    .buttonBorderShape(.capsule)
                    .controlSize(.small)
                    .tint(.secondary)
                    .transition(.opacity)
                }
            }
            .frame(height: 28)
            .motion(Theme.Motion.standard, value: contactName)
        }
        .frame(maxWidth: .infinity)
    }
}

/// Deletes the last digit; holding it clears the number.
private struct DeleteKey: View {
    let isVisible: Bool
    let onDelete: () -> Void
    let onClear: () -> Void

    var body: some View {
        Button(action: onDelete) {
            Image(systemName: "delete.left.fill")
                .font(.title2)
                .foregroundStyle(.secondary)
                .frame(width: 78, height: 78)
                .contentShape(Rectangle())
        }
        .buttonStyle(PressableButtonStyle())
        .simultaneousGesture(LongPressGesture(minimumDuration: 0.6).onEnded { _ in onClear() })
        .opacity(isVisible ? 1 : 0)
        .disabled(!isVisible)
        .motion(Theme.Motion.snappy, value: isVisible)
        .accessibilityLabel(Text("Löschen"))
        .accessibilityHint(Text("Gedrückt halten löscht die ganze Nummer"))
    }
}

/// Quiet, one-line hint when calls can't go out right now.
struct BridgeStatusBanner: View {
    @Environment(BridgeConnection.self) private var bridge

    var body: some View {
        Group {
            switch bridge.status {
            case .online(sipRegistered: true), .unpaired:
                EmptyView()
            case .online(sipRegistered: false):
                banner(tone: .warning, text: "Bridge erreichbar, aber nicht an der FRITZ!Box angemeldet")
            case .connecting:
                banner(tone: .neutral, text: "Verbinde mit der Bridge …", busy: true)
            case .offline:
                banner(tone: .negative, text: "Bridge nicht erreichbar")
            case .rejected:
                banner(tone: .negative, text: "Bridge kennt dieses iPhone nicht mehr – bitte neu koppeln")
            }
        }
        .motion(Theme.Motion.standard, value: bridge.status)
    }

    private func banner(tone: StatusIndicator.Tone, text: LocalizedStringKey, busy: Bool = false) -> some View {
        StatusIndicator(tone: tone, label: text, isBusy: busy)
            .padding(.vertical, Theme.Space.s2)
            .padding(.horizontal, Theme.Space.s4)
            .frame(maxWidth: .infinity)
            .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: Theme.Radius.lg))
            .transition(.opacity.combined(with: .move(edge: .top)))
    }
}
