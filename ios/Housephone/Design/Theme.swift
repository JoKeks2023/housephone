import SwiftUI

/// Design tokens for Housephone, derived from the jorisconrad design
/// language: dark-first neutral grays, one accent (teal), semantic colors
/// that mean the same thing everywhere, a 4-pt spacing scale, small radii.
enum Theme {
    enum Space {
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

    /// The one accent. Defined in the asset catalog with light and dark
    /// variants (#0D9488 / #14B8A6).
    static let accent = Color.accentColor

    /// Semantic: starting or accepting a call.
    static let call = Color(red: 0x2E / 255, green: 0xA0 / 255, blue: 0x43 / 255)
    /// Semantic: ending or declining a call, destructive actions.
    static let danger = Color(red: 0xE5 / 255, green: 0x48 / 255, blue: 0x4D / 255)
    /// Semantic: degraded but working (e.g. bridge online, FRITZ!Box not registered).
    static let warning = Color(red: 0xD2 / 255, green: 0x99 / 255, blue: 0x22 / 255)

    static let textPrimary = Color.primary
    static let textSecondary = Color.secondary

    /// Springs: critically damped for triggered changes, a little bounce
    /// only after direct manipulation.
    enum Motion {
        static let standard = Animation.spring(duration: 0.35, bounce: 0)
        static let snappy = Animation.spring(duration: 0.2, bounce: 0)
        static let momentum = Animation.spring(duration: 0.4, bounce: 0.2)
    }
}

extension View {
    /// Applies `animation` unless the user asked for reduced motion.
    func motion<V: Equatable>(_ animation: Animation, value: V) -> some View {
        modifier(ReducedMotionAnimation(animation: animation, value: value))
    }
}

private struct ReducedMotionAnimation<V: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let animation: Animation
    let value: V

    func body(content: Content) -> some View {
        content.animation(reduceMotion ? .easeInOut(duration: 0.15) : animation, value: value)
    }
}
