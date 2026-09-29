import SwiftUI

/// Status as dot + label: color never carries meaning alone.
struct StatusIndicator: View {
    enum Tone {
        case positive
        case warning
        case negative
        case neutral

        var color: Color {
            switch self {
            case .positive: Theme.call
            case .warning: Theme.warning
            case .negative: Theme.danger
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

/// Circle with initials, or a phone glyph for unknown numbers.
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
                .fill(Color.accentColor.opacity(0.16))
            if let initials {
                Text(initials)
                    .font(.system(size: size * 0.38, weight: .semibold, design: .rounded))
                    .foregroundStyle(Color.accentColor)
            } else {
                Image(systemName: "phone.fill")
                    .font(.system(size: size * 0.36, weight: .medium))
                    .foregroundStyle(Color.accentColor)
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

/// The round, tinted glass button used on the call screen.
struct CallControlButton: View {
    let symbol: String
    let label: LocalizedStringKey
    var isOn = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: Theme.Space.s2) {
                Image(systemName: symbol)
                    .font(.title2.weight(.medium))
                    .symbolVariant(isOn ? .fill : .none)
                    .contentTransition(.symbolEffect(.replace))
                    .frame(width: 72, height: 72)
                    .foregroundStyle(isOn ? Color.black : Color.white)
                    .background {
                        if isOn { Circle().fill(Color.white) }
                    }
                    .glassEffect(.regular.interactive(), in: .circle)
                Text(label)
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(.white.opacity(0.85))
            }
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Text(label))
        .accessibilityAddTraits(isOn ? .isSelected : [])
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
                            // Light-catching top edge for a tactile, physical feel.
                            Circle()
                                .strokeBorder(
                                    LinearGradient(colors: [.white.opacity(0.35), .clear], startPoint: .top, endPoint: .center),
                                    lineWidth: 1
                                )
                        }
                        .shadow(color: kind.color.opacity(0.35), radius: 12, y: 4)
                }
        }
        .buttonStyle(PressableButtonStyle())
        .disabled(!isEnabled)
        .opacity(isEnabled ? 1 : 0.4)
        .accessibilityLabel(Text(label))
    }
}

/// Scales down on press, starting at touch-down.
struct PressableButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.94 : 1)
            .animation(Theme.Motion.snappy, value: configuration.isPressed)
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
