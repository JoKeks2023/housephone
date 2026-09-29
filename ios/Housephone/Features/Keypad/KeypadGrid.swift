import AudioToolbox
import SwiftUI

/// The 3×4 phone keypad. Used for dialing (solid keys) and for DTMF
/// during a call (glass keys over the call background).
struct KeypadGrid: View {
    enum Style {
        case solid
        case glass
    }

    struct Key: Identifiable {
        let digit: String
        let letters: String
        /// Long-press alternative, e.g. `+` on `0`.
        var alternate: String?

        var id: String { digit }
    }

    static let keys: [Key] = [
        Key(digit: "1", letters: ""), Key(digit: "2", letters: "ABC"), Key(digit: "3", letters: "DEF"),
        Key(digit: "4", letters: "GHI"), Key(digit: "5", letters: "JKL"), Key(digit: "6", letters: "MNO"),
        Key(digit: "7", letters: "PQRS"), Key(digit: "8", letters: "TUV"), Key(digit: "9", letters: "WXYZ"),
        Key(digit: "*", letters: ""), Key(digit: "0", letters: "+", alternate: "+"), Key(digit: "#", letters: ""),
    ]

    var style: Style = .solid
    var keySize: CGFloat = 78
    let onKey: (String) -> Void

    var body: some View {
        Grid(horizontalSpacing: Theme.Space.s6, verticalSpacing: Theme.Space.s4) {
            ForEach(0..<4) { row in
                GridRow {
                    ForEach(Self.keys[(row * 3)..<(row * 3 + 3)]) { key in
                        KeypadKey(key: key, style: style, size: keySize, onKey: onKey)
                    }
                }
            }
        }
    }
}

private struct KeypadKey: View {
    let key: KeypadGrid.Key
    let style: KeypadGrid.Style
    let size: CGFloat
    let onKey: (String) -> Void

    @State private var presses = 0
    @State private var isPressed = false
    @State private var firedAlternate = false

    var body: some View {
        VStack(spacing: 0) {
            Text(key.digit)
                .font(.system(size: size * 0.42, weight: .regular))
                .monospacedDigit()
            if !key.letters.isEmpty {
                Text(key.letters)
                    .font(.system(size: size * 0.13, weight: .semibold))
                    .tracking(1.5)
                    .foregroundStyle(.secondary)
            }
        }
        .foregroundStyle(style == .glass ? Color.white : Color.primary)
        .frame(width: size, height: size)
        .background {
            if style == .solid {
                Circle().fill(isPressed ? Color(.systemFill) : Color(.tertiarySystemFill))
            }
        }
        .modifier(GlassKeyModifier(enabled: style == .glass))
        .contentShape(Circle())
        .scaleEffect(isPressed ? 0.94 : 1)
        .animation(Theme.Motion.snappy, value: isPressed)
        .onLongPressGesture(minimumDuration: 0.45) {
            guard let alternate = key.alternate else { return }
            firedAlternate = true
            emit(alternate)
        } onPressingChanged: { pressing in
            isPressed = pressing
            if pressing {
                firedAlternate = false
                // Standard keys fire on touch-down, like the Phone app.
                if key.alternate == nil { emit(key.digit) }
            } else if key.alternate != nil, !firedAlternate {
                // Keys with an alternate wait for release to tell tap from hold.
                emit(key.digit)
            }
        }
        .sensoryFeedback(.impact(flexibility: .soft, intensity: 0.6), trigger: presses)
        .accessibilityElement()
        .accessibilityLabel(Text(key.digit))
        .accessibilityHint(key.alternate != nil ? Text("Gedrückt halten für +") : Text(verbatim: ""))
        .accessibilityAddTraits(.isButton)
        .accessibilityAction { emit(key.digit) }
    }

    private func emit(_ value: String) {
        presses += 1
        KeypadTone.play(value)
        onKey(value)
    }
}

private struct GlassKeyModifier: ViewModifier {
    let enabled: Bool

    func body(content: Content) -> some View {
        if enabled {
            content.glassEffect(.regular.interactive(), in: .circle)
        } else {
            content
        }
    }
}

/// The system keypad tones (respect the ring/silent switch).
enum KeypadTone {
    static func play(_ key: String) {
        let id: SystemSoundID? = switch key {
        case "0"..."9": SystemSoundID(1200 + Int(key)!)
        case "*": 1210
        case "#": 1211
        default: nil
        }
        if let id { AudioServicesPlaySystemSound(id) }
    }
}
