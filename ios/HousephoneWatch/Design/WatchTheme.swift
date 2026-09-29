import SwiftUI

/// Design tokens of the watch app. The same language as the iPhone app —
/// true-black canvas, one teal accent, semantic colors that always mean
/// the same thing — tuned for a glanceable, one-hand screen.
enum WatchTheme {
    enum Space {
        /// Between a title and its subtitle only.
        static let hairline: CGFloat = 2
        static let s1: CGFloat = 4
        static let s2: CGFloat = 8
        static let s3: CGFloat = 12
        static let s4: CGFloat = 16
    }

    enum Radius {
        static let sm: CGFloat = 6
        static let md: CGFloat = 8
    }

    /// Smallest comfortable touch target on the watch.
    static let minTarget: CGFloat = 40

    /// Starting or accepting a call.
    static let call = Color(red: 0x2E / 255, green: 0xA0 / 255, blue: 0x43 / 255)
    /// Ending or declining a call.
    static let danger = Color(red: 0xE5 / 255, green: 0x48 / 255, blue: 0x4D / 255)
    /// Degraded but working.
    static let warning = Color(red: 0xD2 / 255, green: 0x99 / 255, blue: 0x22 / 255)

    enum Motion {
        static let standard = Animation.spring(duration: 0.35, bounce: 0)
        static let snappy = Animation.spring(duration: 0.2, bounce: 0)
        /// Press feedback: on at touch-down, easing out on release.
        static let release = Animation.easeOut(duration: 0.25)
    }
}

extension View {
    /// Applies `animation` unless the user asked for reduced motion; then
    /// a short cross-fade.
    func motion<V: Equatable>(_ animation: Animation, value: V) -> some View {
        modifier(WatchReducedMotion(animation: animation, value: value))
    }
}

private struct WatchReducedMotion<V: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let animation: Animation
    let value: V

    func body(content: Content) -> some View {
        content.animation(reduceMotion ? .easeInOut(duration: 0.15) : animation, value: value)
    }
}

/// Scales and dims at touch-down, eases back on release: every tappable
/// shape on the watch answers the finger.
struct WatchPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.95 : 1)
            .opacity(configuration.isPressed ? 0.7 : 1)
            .animation(configuration.isPressed ? nil : WatchTheme.Motion.release, value: configuration.isPressed)
    }
}

/// Status as dot + label: color never carries meaning alone.
struct WatchStatus: View {
    enum Tone {
        case positive
        case warning
        case negative
        case neutral

        var color: Color {
            switch self {
            case .positive: WatchTheme.call
            case .warning: WatchTheme.warning
            case .negative: WatchTheme.danger
            case .neutral: .secondary
            }
        }
    }

    let tone: Tone
    let label: LocalizedStringKey
    var isBusy = false

    var body: some View {
        HStack(spacing: WatchTheme.Space.s2) {
            if isBusy {
                ProgressView()
                    .frame(width: 12, height: 12)
            } else {
                Circle()
                    .fill(tone.color)
                    .frame(width: 7, height: 7)
            }
            Text(label)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(2)
        }
        .accessibilityElement(children: .combine)
    }
}

/// Round call button: green to start/accept, red to end/decline.
struct WatchCallButton: View {
    enum Kind {
        case start
        case end

        var color: Color {
            switch self {
            case .start: WatchTheme.call
            case .end: WatchTheme.danger
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
    var size: CGFloat = 52
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: kind.symbol)
                .font(.system(size: size * 0.4, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: size, height: size)
                .background(Circle().fill(kind.color.gradient))
        }
        .buttonStyle(WatchPressStyle())
        .accessibilityLabel(Text(label))
    }
}

extension TimeInterval {
    /// `1:05` or `1:02:05`.
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
