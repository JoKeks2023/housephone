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

    /// The fixed part of the layout below: banner with top padding up to
    /// 64 (two lines on an SE), two spacers 2 × 24,
    /// number display 82, grid row spacing 3 × 16, call row padding
    /// 16 + 32. The rest scales with the key size: four key rows plus the
    /// call row = 5 keys. Per-device numbers are in the T-0007 report.
    private static let fixedHeight: CGFloat = 290

    /// Keys shrink on short screens (iPhone SE) instead of pushing the
    /// call button under the tab bar; 78 pt is the Phone app size.
    private static func keySize(for height: CGFloat) -> CGFloat {
        min(78, max(56, (height - fixedHeight) / 5))
    }

    var body: some View {
        @Bindable var appModel = appModel
        let number = appModel.keypadNumber

        GeometryReader { proxy in
            let keySize = Self.keySize(for: proxy.size.height)
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

                KeypadGrid(keySize: keySize) { key in
                    appModel.keypadNumber.append(key)
                }

                HStack {
                    Color.clear.frame(width: keySize, height: keySize)
                        .frame(maxWidth: .infinity)

                    CallActionButton(kind: .start, label: "Anrufen", size: min(76, keySize)) {
                        call()
                    }
                    .frame(maxWidth: .infinity)

                    DeleteKey(isVisible: !number.isEmpty, size: keySize) {
                        guard !appModel.keypadNumber.isEmpty else { return }
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
            .frame(maxWidth: .infinity, maxHeight: .infinity)
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

    /// 38 pt at the default size, growing and shrinking with Dynamic Type.
    @ScaledMetric(relativeTo: .largeTitle) private var numberSize: CGFloat = 38

    var body: some View {
        VStack(spacing: Theme.Space.s2) {
            // No numeric-text roll while typing: dozens of times a day,
            // the digit should just be there.
            Text(number.isEmpty ? " " : number)
                .font(.system(size: numberSize, weight: .regular))
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.45)
                .truncationMode(.head)
                .accessibilityLabel(number.isEmpty ? Text("Keine Nummer eingegeben") : Text.spokenNumber(number))

            ZStack {
                if let contactName {
                    Text(contactName)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Theme.accentText)
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
            .frame(minHeight: 28)
            .motion(Theme.Motion.standard, value: contactName)
        }
        .frame(maxWidth: .infinity)
    }
}

/// Deletes the last digit; holding it clears the number.
private struct DeleteKey: View {
    let isVisible: Bool
    let size: CGFloat
    let onDelete: () -> Void
    let onClear: () -> Void

    var body: some View {
        Button(action: onDelete) {
            Image(systemName: "delete.left.fill")
                .font(.title2)
                .foregroundStyle(.secondary)
                .frame(width: size, height: size)
                .contentShape(Rectangle())
        }
        .buttonStyle(PressableButtonStyle())
        .simultaneousGesture(LongPressGesture(minimumDuration: 0.6).onEnded { _ in onClear() })
        .opacity(isVisible ? 1 : 0)
        .disabled(!isVisible)
        .motion(Theme.Motion.snappy, value: isVisible)
        .accessibilityLabel(Text("Löschen"))
        .accessibilityHint(Text("Gedrückt halten löscht die ganze Nummer"))
        .accessibilityAction(named: Text("Nummer löschen")) { onClear() }
    }
}

/// Quiet, one-line hint when calls can't go out right now. When the
/// user has to act, it leads to where they can.
struct BridgeStatusBanner: View {
    @Environment(BridgeConnection.self) private var bridge
    @Environment(DirectPhone.self) private var direct
    @Environment(AppModel.self) private var appModel

    var body: some View {
        Group {
            if direct.isEnabled {
                directBanner
            } else {
                bridgeBanner
            }
        }
        .motion(Theme.Motion.standard, value: bridge.status)
        .motion(Theme.Motion.standard, value: direct.status)
    }

    /// Mode without bridge (ADR-0005).
    @ViewBuilder
    private var directBanner: some View {
        switch direct.status {
        case .ready, .off:
            EmptyView()
        case .connecting:
            banner(tone: .neutral, text: "Melde an der FRITZ!Box an …", busy: true)
        case .notAtHome:
            banner(tone: .warning, text: "Nur im Heim-WLAN verfügbar – unterwegs nicht erreichbar")
        case .wrongPassword, .rejected:
            Button {
                appModel.selectedTab = .settings
            } label: {
                banner(tone: .negative, text: "Die FRITZ!Box lehnt die Anmeldung ab. Zum Prüfen tippen.", showsChevron: true)
            }
            .buttonStyle(RowButtonStyle())
            .accessibilityHint(Text("Öffnet die Einstellungen"))
        }
    }

    @ViewBuilder
    private var bridgeBanner: some View {
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
            Button {
                appModel.selectedTab = .settings
            } label: {
                banner(tone: .negative, text: "Die Bridge kennt dieses iPhone nicht mehr. Zum Neu-Koppeln tippen.", showsChevron: true)
            }
            .buttonStyle(RowButtonStyle())
            .accessibilityHint(Text("Öffnet die Einstellungen"))
        }
    }

    private func banner(tone: StatusIndicator.Tone, text: LocalizedStringKey, busy: Bool = false, showsChevron: Bool = false) -> some View {
        HStack(spacing: Theme.Space.s2) {
            StatusIndicator(tone: tone, label: text, isBusy: busy)
                .frame(maxWidth: .infinity, alignment: .leading)
            if showsChevron {
                Image(systemName: "chevron.forward")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
        }
        .padding(.vertical, Theme.Space.s2)
        .padding(.horizontal, Theme.Space.s4)
        .frame(maxWidth: .infinity)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: Theme.Radius.md))
        .transition(.opacity.combined(with: .move(edge: .top)))
    }
}
