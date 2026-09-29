import SwiftUI

/// Status as dot + label: color never carries meaning alone.
struct StatusIndicator: View {
    enum Tone {
        case positive
        case warning
        case negative
        case neutral

        /// Dot color: 3:1 or better against the background (a graphic).
        var color: Color {
            switch self {
            case .positive: Theme.call
            case .warning: Theme.warningText
            case .negative: Theme.dangerText
            case .neutral: .secondary
            }
        }
    }

    let tone: Tone
    let label: LocalizedStringKey
    var isBusy = false

    var body: some View {
        HStack(spacing: Theme.Space.s2) {
            if isBusy {
                ProgressView()
                    .controlSize(.mini)
            } else {
                Circle()
                    .fill(tone.color)
                    .frame(width: 8, height: 8)
            }
            Text(label)
                .font(.subheadline.weight(.medium))
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

/// Circle with initials, or a person glyph for unknown callers. Neutral,
/// like Phone and Contacts: the accent stays reserved for actions.
struct AvatarView: View {
    let name: String?
    var size: CGFloat = 44

    private var initials: String? {
        guard let name else { return nil }
        let parts = name.split(separator: " ").prefix(2)
        let letters = parts.compactMap { $0.first.map(String.init) }.joined()
        return letters.isEmpty ? nil : letters.uppercased()
    }

    var body: some View {
        ZStack {
            Circle()
                .fill(.fill.tertiary)
            if let initials {
                Text(initials)
                    .font(.system(size: size * 0.38, weight: .semibold, design: .rounded))
                    .foregroundStyle(.secondary)
            } else {
                Image(systemName: "person.fill")
                    .font(.system(size: size * 0.4, weight: .medium))
                    .foregroundStyle(.secondary)
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// Friendly empty or error state with an optional action.
struct EmptyStateView<Actions: View>: View {
    let symbol: String
    let title: LocalizedStringKey
    let message: LocalizedStringKey
    @ViewBuilder var actions: Actions

    var body: some View {
        ContentUnavailableView {
            Label(title, systemImage: symbol)
        } description: {
            Text(message)
        } actions: {
            actions
        }
    }
}

extension EmptyStateView where Actions == EmptyView {
    init(symbol: String, title: LocalizedStringKey, message: LocalizedStringKey) {
        self.init(symbol: symbol, title: title, message: message) { EmptyView() }
    }
}

/// Identifies the glass of a call control so it can morph into the keypad.
struct GlassMorph {
    let id: String
    let namespace: Namespace.ID
}

/// The round glass button used on the call screen. "On" reads as filled
/// white; the change cross-fades with a Magic Replace of the symbol.
struct CallControlButton: View {
    let symbol: String
    let label: LocalizedStringKey
    var isOn = false
    var morph: GlassMorph?
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: Theme.Space.s2) {
                Image(systemName: symbol)
                    .font(.title2.weight(.medium))
                    .symbolVariant(isOn ? .fill : .none)
                    .contentTransition(.symbolEffect(.replace.magic(fallback: .replace)))
                    .frame(width: 72, height: 72)
                    .foregroundStyle(isOn ? Color.black : Color.white)
                    .background {
                        Circle().fill(Color.white).opacity(isOn ? 1 : 0)
                    }
                    .glassEffect(.regular.interactive(), in: .circle)
                    .glassMorph(morph)
                Text(label)
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(.white.opacity(0.85))
            }
        }
        .buttonStyle(.plain)
        .motion(Theme.Motion.snappy, value: isOn)
        .sensoryFeedback(.selection, trigger: isOn)
        .accessibilityLabel(Text(label))
        .accessibilityAddTraits(isOn ? .isSelected : [])
    }
}

extension View {
    /// Gives this view's glass an identity so it can morph within a
    /// `GlassEffectContainer`. No-op without `morph`.
    @ViewBuilder
    func glassMorph(_ morph: GlassMorph?) -> some View {
        if let morph {
            glassEffectID(morph.id, in: morph.namespace)
        } else {
            self
        }
    }
}

/// Big round button to start (green) or end (red) a call.
struct CallActionButton: View {
    enum Kind {
        case start
        case end

        var color: Color {
            switch self {
            case .start: Theme.call
            case .end: Theme.danger
            }
        }

        var symbol: String {
            switch self {
            case .start: "phone.fill"
            case .end: "phone.down.fill"
            }
        }
    }

    let kind: Kind
    let label: LocalizedStringKey
    var size: CGFloat = 76
    var isEnabled = true
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: kind.symbol)
                .font(.system(size: size * 0.38, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: size, height: size)
                .background {
                    Circle()
                        .fill(kind.color.gradient)
                        .overlay {
                            // Tactile construction: a ring one step darker than
                            // the fill and a light-catching top edge.
                            Circle()
                                .strokeBorder(kind.color.mix(with: .black, by: 0.2), lineWidth: 1)
                        }
                        .overlay {
                            Circle()
                                .inset(by: 1)
                                .strokeBorder(
                                    LinearGradient(colors: [.white.opacity(0.35), .clear], startPoint: .top, endPoint: .center),
                                    lineWidth: 1
                                )
                        }
                        .shadow(color: kind.color.opacity(0.18), radius: 12, y: 4)
                }
        }
        .buttonStyle(PressableButtonStyle())
        .disabled(!isEnabled)
        .opacity(isEnabled ? 1 : 0.4)
        .accessibilityLabel(Text(label))
    }
}

/// Scales down at touch-down and eases back on release.
struct PressableButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? Theme.pressedScale : 1)
            .pressAnimation(isPressed: configuration.isPressed)
    }
}

/// A list row that calls on tap: dims at touch-down like system rows,
/// without tinting the whole label in the accent color.
struct RowButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.55 : 1)
            .pressAnimation(isPressed: configuration.isPressed)
    }
}

/// Phone-app style dates for call lists: time today, "Gestern",
/// the weekday within the last week, otherwise the date.
enum CallDate {
    static func text(for date: Date, now: Date = .now, calendar: Calendar = .current) -> String {
        if calendar.isDateInToday(date) {
            return date.formatted(.dateTime.hour().minute())
        }
        if calendar.isDateInYesterday(date) {
            return String(localized: "Gestern")
        }
        if let days = calendar.dateComponents([.day], from: calendar.startOfDay(for: date), to: calendar.startOfDay(for: now)).day,
           days < 7 {
            return date.formatted(.dateTime.weekday(.wide))
        }
        return date.formatted(.dateTime.day().month(.twoDigits).year(.twoDigits))
    }
}

extension AttributedString {
    /// A phone number read digit by digit by VoiceOver
    /// ("plus 4 9 3 0 …" instead of "three hundred million …").
    static func spokenNumber(_ number: String) -> AttributedString {
        var spoken = AttributedString(number)
        spoken.accessibilitySpeechSpellsOutCharacters = true
        return spoken
    }
}

extension Text {
    /// A phone number read digit by digit by VoiceOver.
    static func spokenNumber(_ number: String) -> Text {
        Text(AttributedString.spokenNumber(number))
    }
}

extension TimeInterval {
    /// `1:05` or `1:02:05`, for call durations.
    var callDurationText: String {
        let total = Int(self.rounded(.down))
        let hours = total / 3600
        let minutes = (total % 3600) / 60
        let seconds = total % 60
        return hours > 0
            ? String(format: "%d:%02d:%02d", hours, minutes, seconds)
            : String(format: "%d:%02d", minutes, seconds)
    }
}
