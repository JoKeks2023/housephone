import SwiftUI

/// Design tokens for Housephone, derived from the jorisconrad design
/// language: dark-first neutral grays, one accent (teal), semantic colors
/// that mean the same thing everywhere, a 4-pt spacing scale, small radii.
enum Theme {
    enum Space {
        /// Between a title and its subtitle only.
        static let hairline: CGFloat = 2
        static let s1: CGFloat = 4
        static let s2: CGFloat = 8
        static let s3: CGFloat = 12
        static let s4: CGFloat = 16
        static let s5: CGFloat = 20
        static let s6: CGFloat = 24
        static let s8: CGFloat = 32
        static let s10: CGFloat = 40
        static let s12: CGFloat = 48
        static let s16: CGFloat = 64
    }

    enum Radius {
        static let sm: CGFloat = 6
        static let md: CGFloat = 8
        static let lg: CGFloat = 12
        static let xl: CGFloat = 16
    }

    /// The one accent, for fills: primary actions, selection, links.
    /// Asset catalog, light and dark variants (#0D9488 / #14B8A6).
    static let accent = Color.accentColor
    /// Accent for text: AA-safe on system backgrounds in light and dark
    /// mode and with Increase Contrast.
    static let accentText = Color("AccentText")

    /// Semantic fills: starting or accepting a call (the -9 step).
    static let call = Color("Call")
    /// Semantic fills: ending or declining a call, destructive actions.
    static let danger = Color("Danger")
    /// Semantic fills: degraded but working.
    static let warning = Color("Warning")

    /// Semantic text: missed calls, errors. AA-safe.
    static let dangerText = Color("DangerText")
    /// Semantic text: warnings. AA-safe.
    static let warningText = Color("WarningText")

    /// Springs: critically damped for triggered changes, a little bounce
    /// only after direct manipulation.
    enum Motion {
        static let standard = Animation.spring(duration: 0.35, bounce: 0)
        static let snappy = Animation.spring(duration: 0.2, bounce: 0)
        static let momentum = Animation.spring(duration: 0.4, bounce: 0.2)
        /// Press feedback: on instantly at touch-down, fading out on release.
        static let release = Animation.easeOut(duration: 0.25)
    }

    /// Scale of pressed buttons (0.95–0.98 reads as physical, not squishy).
    static let pressedScale: CGFloat = 0.96
}

extension View {
    /// Applies `animation` unless the user asked for reduced motion.
    func motion<V: Equatable>(_ animation: Animation, value: V) -> some View {
        modifier(ReducedMotionAnimation(animation: animation, value: value))
    }

    /// Press feedback that starts at touch-down and eases out on release.
    func pressAnimation(isPressed: Bool) -> some View {
        modifier(PressAnimation(isPressed: isPressed))
    }
}

/// Runs `body` with `animation`, or a short cross-fade with Reduce Motion.
@MainActor
func withMotion(_ animation: Animation, _ body: () -> Void) {
    let reduce = UIAccessibility.isReduceMotionEnabled
    withAnimation(reduce ? .easeInOut(duration: 0.15) : animation, body)
}

private struct ReducedMotionAnimation<V: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let animation: Animation
    let value: V

    func body(content: Content) -> some View {
        content.animation(reduceMotion ? .easeInOut(duration: 0.15) : animation, value: value)
    }
}

private struct PressAnimation: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let isPressed: Bool

    func body(content: Content) -> some View {
        content.animation(isPressed || reduceMotion ? nil : Theme.Motion.release, value: isPressed)
    }
}
