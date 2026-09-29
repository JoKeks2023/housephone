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
                .contentTransition(.numericText())
            if !number.isEmpty {
                Button {
                    number.removeLast()
                    WKInterfaceDevice.current().play(.click)
                } label: {
                    Image(systemName: "delete.left")
                        .font(.body)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                .accessibilityLabel(Text("Löschen"))
            }
        }
        .frame(height: 26)
        .animation(WatchTheme.Motion.snappy, value: number)
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

/// The 12 keys, sized to the watch.
struct WatchKeyGrid: View {
    let onKey: (String) -> Void
    let onLongPressZero: () -> Void

    private let rows = [["1", "2", "3"], ["4", "5", "6"], ["7", "8", "9"], ["*", "0", "#"]]

    var body: some View {
        Grid(horizontalSpacing: WatchTheme.Space.s1, verticalSpacing: WatchTheme.Space.s1) {
            ForEach(rows, id: \.self) { row in
                GridRow {
                    ForEach(row, id: \.self) { key in
                        key == "0" ? AnyView(zeroKey) : AnyView(button(for: key))
                    }
                }
            }
        }
    }

    private func button(for key: String) -> some View {
        Button {
            onKey(key)
        } label: {
            keyLabel(key)
        }
        .buttonStyle(KeyButtonStyle())
        .accessibilityLabel(Text(key))
    }

    private var zeroKey: some View {
        keyLabel("0", subtitle: "+")
            .contentShape(Rectangle())
            .onTapGesture { onKey("0") }
            .onLongPressGesture(minimumDuration: 0.5) { onLongPressZero() }
            .accessibilityElement()
            .accessibilityLabel(Text("0"))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction(named: Text("Plus")) { onLongPressZero() }
    }

    private func keyLabel(_ key: String, subtitle: String? = nil) -> some View {
        VStack(spacing: 0) {
            Text(key)
                .font(.title3.weight(.medium).monospacedDigit())
            if let subtitle {
                Text(subtitle)
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, minHeight: 30)
        .background(RoundedRectangle(cornerRadius: 8).fill(Color.white.opacity(0.12)))
    }
}

private struct KeyButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.6 : 1)
            .scaleEffect(configuration.isPressed ? 0.95 : 1)
            .animation(WatchTheme.Motion.snappy, value: configuration.isPressed)
    }
}
