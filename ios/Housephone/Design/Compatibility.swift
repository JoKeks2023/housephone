import SwiftUI

// The app runs from iOS 17, for iPads that stop there. Liquid Glass and
// the newer tab and toolbar APIs need iOS 26 (the Tab API 18); below that
// the same views use materials and the classic styles.

extension View {
    /// Liquid Glass in `shape`; a thin material before iOS 26. `enabled:
    /// false` keeps the view's place in a glass group without glass.
    @ViewBuilder
    func glassSurface(in shape: some Shape, interactive: Bool = false, enabled: Bool = true) -> some View {
        if #available(iOS 26, *) {
            glassEffect(enabled ? .regular.interactive(interactive) : .identity, in: shape)
        } else if enabled {
            background(.ultraThinMaterial, in: shape)
        } else {
            self
        }
    }

    /// `.glass` and `.glassProminent` buttons; bordered styles before iOS 26.
    @ViewBuilder
    func glassButtonStyle(prominent: Bool = false) -> some View {
        if #available(iOS 26, *) {
            if prominent {
                buttonStyle(.glassProminent)
            } else {
                buttonStyle(.glass)
            }
        } else if prominent {
            buttonStyle(.borderedProminent)
        } else {
            buttonStyle(.bordered)
        }
    }

    /// Symbol swap with the iOS 18 "magic" replace, plain replace before.
    @ViewBuilder
    func magicReplaceTransition() -> some View {
        if #available(iOS 18, *) {
            contentTransition(.symbolEffect(.replace.magic(fallback: .replace)))
        } else {
            contentTransition(.symbolEffect(.replace))
        }
    }

    /// A bar above the content that scrolls under it: `safeAreaBar` from
    /// iOS 26, an inset on the bar material before.
    @ViewBuilder
    func topAccessoryBar(@ViewBuilder _ bar: () -> some View) -> some View {
        if #available(iOS 26, *) {
            safeAreaBar(edge: .top, content: bar)
        } else {
            safeAreaInset(edge: .top) {
                bar().background(.bar)
            }
        }
    }

    /// The subtitle under the navigation title, from iOS 26.
    @ViewBuilder
    func navigationSubtitleIfAvailable(_ subtitle: Text) -> some View {
        if #available(iOS 26, *) {
            navigationSubtitle(subtitle)
        } else {
            self
        }
    }

    /// Lets the tab bar shrink while scrolling down, from iOS 26.
    @ViewBuilder
    func minimizesTabBarOnScroll() -> some View {
        if #available(iOS 26, *) {
            tabBarMinimizeBehavior(.onScrollDown)
        } else {
            self
        }
    }
}

extension Color {
    /// The color mixed with black: `mix(with:by:)` from iOS 18, the same
    /// arithmetic on the resolved components before.
    func darkened(by amount: Double) -> Color {
        if #available(iOS 18, *) {
            return mix(with: .black, by: amount)
        }
        var red: CGFloat = 0, green: CGFloat = 0, blue: CGFloat = 0, alpha: CGFloat = 0
        guard UIColor(self).getRed(&red, green: &green, blue: &blue, alpha: &alpha) else { return self }
        let keep = 1 - amount
        return Color(red: red * keep, green: green * keep, blue: blue * keep, opacity: alpha)
    }
}

/// `GlassEffectContainer` from iOS 26, so neighbouring glass blends and
/// morphs; before that just the content.
struct GlassGroup<Content: View>: View {
    var spacing: CGFloat?
    @ViewBuilder var content: Content

    var body: some View {
        if #available(iOS 26, *) {
            GlassEffectContainer(spacing: spacing) { content }
        } else {
            content
        }
    }
}
