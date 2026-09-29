import HousephoneKit
import SwiftUI
import WatchKit

/// Compact keypad: number on top, 4 × 3 keys, call button in the bottom bar.
struct WatchKeypadView: View {
    @Environment(WatchCallCenter.self) private var callCenter
    @Environment(RecentCalls.self) private var recents
    @State private var number = ""

    var body: some View {
        VStack(spacing: WatchTheme.Space.s1) {
            display
            WatchKeyGrid { key in
                append(key)
            } onLongPressZero: {
                append("+")
            }
        }
        .padding(.horizontal, WatchTheme.Space.s1)
        .toolbar {
            ToolbarItem(placement: .bottomBar) {
                WatchCallButton(kind: .start, label: "Anrufen", size: 44) {
                    call()
                }
            }
        }
    }

    private var display: some View {
        HStack(spacing: WatchTheme.Space.s1) {
            Text(number.isEmpty ? String(localized: "Nummer") : number)
                .font(.title3.monospacedDigit().weight(.medium))
                .foregroundStyle(number.isEmpty ? .secondary : .primary)
                .lineLimit(1)
                .minimumScaleFactor(0.5)
                .truncationMode(.head)
                .frame(maxWidth: .infinity)
                .accessibilityLabel(number.isEmpty ? Text("Keine Nummer eingegeben") : Text(AttributedString.spokenNumber(number)))
            if !number.isEmpty {
                Button {
                    guard !number.isEmpty else { return }
                    number.removeLast()
                    WKInterfaceDevice.current().play(.click)
                } label: {
                    Image(systemName: "delete.left")
                        .font(.body)
                        .frame(width: 32, height: 32)
                        .contentShape(Rectangle())
                }
                .buttonStyle(WatchPressStyle())
                .foregroundStyle(.secondary)
                .accessibilityLabel(Text("Löschen"))
                .accessibilityAction(named: Text("Nummer löschen")) { number = "" }
                .transition(.opacity)
            }
        }
        .frame(height: 32)
        .motion(WatchTheme.Motion.snappy, value: number.isEmpty)
    }

    private func append(_ key: String) {
        guard number.count < 32 else { return }
        number.append(key)
        WKInterfaceDevice.current().play(.click)
    }

    private func call() {
        if number.isEmpty, let last = recents.lastNumber {
            // Like the iPhone: an empty keypad fills in the last number.
            number = last
            return
        }
        let dialed = number
        Task {
            if await callCenter.startCall(to: dialed) {
                number = ""
            }
        }
    }
}

extension AttributedString {
    /// A phone number read digit by digit by VoiceOver.
    static func spokenNumber(_ number: String) -> AttributedString {
        var spoken = AttributedString(number)
        spoken.accessibilitySpeechSpellsOutCharacters = true
        return spoken
    }
}

/// The 12 keys, filling the height the watch has. On a 41 mm watch that
/// is about 53 × 30 pt per key; bigger watches get taller keys.
struct WatchKeyGrid: View {
    let onKey: (String) -> Void
    let onLongPressZero: () -> Void

    private let rows = [["1", "2", "3"], ["4", "5", "6"], ["7", "8", "9"], ["*", "0", "#"]]

    var body: some View {
        Grid(horizontalSpacing: WatchTheme.Space.s1, verticalSpacing: WatchTheme.Space.s1) {
            ForEach(rows, id: \.self) { row in
                GridRow {
                    ForEach(row, id: \.self) { key in
                        if key == "0" {
                            ZeroKey(onKey: onKey, onPlus: onLongPressZero)
                        } else {
                            button(for: key)
                        }
                    }
                }
            }
        }
    }

    private func button(for key: String) -> some View {
        Button {
            onKey(key)
        } label: {
            WatchKeyLabel(key: key)
        }
        .buttonStyle(WatchPressStyle())
        .accessibilityLabel(Text(Self.spokenName(key)))
    }

    static func spokenName(_ key: String) -> LocalizedStringKey {
        switch key {
        case "*": "Stern"
        case "#": "Raute"
        default: LocalizedStringKey(key)
        }
    }
}

/// "0", or "+" when held — pressed state like the other keys.
private struct ZeroKey: View {
    let onKey: (String) -> Void
    let onPlus: () -> Void

    @State private var isPressed = false
    @State private var firedPlus = false

    var body: some View {
        WatchKeyLabel(key: "0", subtitle: "+")
            .scaleEffect(isPressed ? 0.95 : 1)
            .opacity(isPressed ? 0.7 : 1)
            .animation(isPressed ? nil : WatchTheme.Motion.release, value: isPressed)
            .contentShape(Rectangle())
            .onLongPressGesture(minimumDuration: 0.5) {
                firedPlus = true
                onPlus()
            } onPressingChanged: { pressing in
                isPressed = pressing
                if pressing {
                    firedPlus = false
                } else if !firedPlus {
                    onKey("0")
                }
            }
            .accessibilityElement()
            .accessibilityLabel(Text("0"))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { onKey("0") }
            .accessibilityAction(named: Text("Plus")) { onPlus() }
    }
}

private struct WatchKeyLabel: View {
    let key: String
    var subtitle: String?

    var body: some View {
        VStack(spacing: 0) {
            Text(key)
                .font(.title3.weight(.medium).monospacedDigit())
            if let subtitle {
                Text(subtitle)
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, minHeight: 30, maxHeight: .infinity)
        .background(RoundedRectangle(cornerRadius: WatchTheme.Radius.md).fill(Color.white.opacity(0.12)))
    }
}
