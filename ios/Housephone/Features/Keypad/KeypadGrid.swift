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

        /// What VoiceOver and Voice Control call the key.
        var spokenName: LocalizedStringKey {
            switch digit {
            case "*": "Stern"
            case "#": "Raute"
            default: LocalizedStringKey(digit)
            }
        }
    }

    static let keys: [Key] = [
        Key(digit: "1", letters: ""), Key(digit: "2", letters: "ABC"), Key(digit: "3", letters: "DEF"),
        Key(digit: "4", letters: "GHI"), Key(digit: "5", letters: "JKL"), Key(digit: "6", letters: "MNO"),
        Key(digit: "7", letters: "PQRS"), Key(digit: "8", letters: "TUV"), Key(digit: "9", letters: "WXYZ"),
        Key(digit: "*", letters: ""), Key(digit: "0", letters: "+", alternate: "+"), Key(digit: "#", letters: ""),
    ]

    /// The glass id of key "5": the keypad button on the call screen uses
    /// it too, so its glass flows into the grid.
    static let centerGlassID = "keypad"

    var style: Style = .solid
    var keySize: CGFloat = 78
    /// Glass identities for morphing (glass style only).
    var glassNamespace: Namespace.ID?
    let onKey: (String) -> Void

    var body: some View {
        Grid(horizontalSpacing: Theme.Space.s6, verticalSpacing: Theme.Space.s4) {
            ForEach(0..<4) { row in
                GridRow {
                    ForEach(Self.keys[(row * 3)..<(row * 3 + 3)]) { key in
                        KeypadKey(key: key, style: style, size: keySize, morph: morph(for: key), onKey: onKey)
                    }
                }
            }
        }
    }

    private func morph(for key: Key) -> GlassMorph? {
        guard style == .glass, let glassNamespace else { return nil }
        return GlassMorph(id: key.digit == "5" ? Self.centerGlassID : "key-\(key.digit)", namespace: glassNamespace)
    }
}

private struct KeypadKey: View {
    let key: KeypadGrid.Key
    let style: KeypadGrid.Style
    let size: CGFloat
    let morph: GlassMorph?
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
        .modifier(GlassKeyModifier(enabled: style == .glass, morph: morph))
        .contentShape(Circle())
        .scaleEffect(isPressed ? 0.97 : 1)
        .pressAnimation(isPressed: isPressed)
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
        .accessibilityLabel(Text(key.spokenName))
        .accessibilityHint(key.alternate != nil ? Text("Gedrückt halten für +") : Text(verbatim: ""))
        .accessibilityAddTraits(.isButton)
        .accessibilityAction { emit(key.digit) }
        .modifier(AlternateAction(key: key, emit: emit))
    }

    private func emit(_ value: String) {
        presses += 1
        KeypadTone.play(value)
        onKey(value)
    }
}

/// "+" on "0" without a long press, for VoiceOver and Voice Control.
private struct AlternateAction: ViewModifier {
    let key: KeypadGrid.Key
    let emit: (String) -> Void

    func body(content: Content) -> some View {
        if let alternate = key.alternate {
            content.accessibilityAction(named: Text("Plus")) { emit(alternate) }
        } else {
            content
        }
    }
}

private struct GlassKeyModifier: ViewModifier {
    let enabled: Bool
    let morph: GlassMorph?

    func body(content: Content) -> some View {
        if enabled {
            content
                .glassEffect(.regular.interactive(), in: .circle)
                .glassMorph(morph)
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
