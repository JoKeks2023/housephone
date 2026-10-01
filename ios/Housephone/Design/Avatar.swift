import SwiftUI
import UIKit

/// The closed set of identity colors for people without a photo. One
/// person always gets the same color (derived from the name), like a
/// favicon: identity, never state.
enum AvatarTint: CaseIterable {
    case blue
    case purple
    case green
    case orange
    case pink
    case teal

    /// The contextual palette, green and orange a step darker so white
    /// initials stay readable (3:1 or better).
    var color: Color {
        switch self {
        case .blue: Color(red: 0x3B / 255, green: 0x82 / 255, blue: 0xF6 / 255)
        case .purple: Color(red: 0x8B / 255, green: 0x5C / 255, blue: 0xF6 / 255)
        case .green: Color(red: 0x1E / 255, green: 0x8E / 255, blue: 0x4A / 255)
        case .orange: Color(red: 0xB8 / 255, green: 0x6E / 255, blue: 0x00 / 255)
        case .pink: Color(red: 0xEC / 255, green: 0x48 / 255, blue: 0x99 / 255)
        case .teal: Color(red: 0x0D / 255, green: 0x94 / 255, blue: 0x88 / 255)
        }
    }

    /// Stable across launches (unlike `hashValue`): FNV-1a over the
    /// name's Unicode scalars.
    static func forName(_ name: String?) -> AvatarTint? {
        guard let name, !name.isEmpty else { return nil }
        var hash: UInt64 = 0xCBF2_9CE4_8422_2325
        for scalar in name.lowercased().unicodeScalars {
            hash ^= UInt64(scalar.value)
            hash = hash &* 0x0000_0100_0000_01B3
        }
        return allCases[Int(hash % UInt64(allCases.count))]
    }
}

/// Initials for a monogram: first letters of the first two words.
enum Monogram {
    static func initials(for name: String?) -> String? {
        guard let name else { return nil }
        let parts = name.split(separator: " ").prefix(2)
        let letters = parts.compactMap { $0.first(where: \.isLetter).map(String.init) }.joined()
        return letters.isEmpty ? nil : letters.uppercased()
    }
}

/// A person: their contact photo, otherwise initials on their identity
/// color, or a person glyph for unknown callers.
struct AvatarView: View {
    let name: String?
    var imageData: Data?
    var size: CGFloat = 44

    var body: some View {
        ZStack {
            if let image = imageData.flatMap(UIImage.init(data:)) {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFill()
            } else if let initials = Monogram.initials(for: name), let tint = AvatarTint.forName(name) {
                Circle()
                    .fill(tint.color.gradient)
                Text(initials)
                    .font(.system(size: size * 0.38, weight: .semibold, design: .rounded))
                    .foregroundStyle(.white)
            } else {
                Circle()
                    .fill(.fill.tertiary)
                Image(systemName: "person.fill")
                    .font(.system(size: size * 0.4, weight: .medium))
                    .foregroundStyle(.secondary)
            }
        }
        .frame(width: size, height: size)
        .clipShape(.circle)
        .accessibilityHidden(true)
    }
}

/// Calm canvas behind the tabs: the system background with the accent's
/// glow from the top (the design language's `accent-glow`, kept at ambient
/// strength). Grouped screens use the grouped background underneath.
struct AppBackground: View {
    enum Style {
        case plain
        case grouped
    }

    var style: Style = .plain
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        ZStack {
            style == .grouped ? Color(.systemGroupedBackground) : Color(.systemBackground)
            RadialGradient(
                colors: [Theme.accent.opacity(colorScheme == .dark ? 0.16 : 0.10), .clear],
                center: UnitPoint(x: 0.5, y: -0.08),
                startRadius: 0,
                endRadius: 460
            )
        }
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }
}

extension View {
    /// Puts a scrolling list or form on the app's background.
    func appBackground(_ style: AppBackground.Style = .plain) -> some View {
        scrollContentBackground(.hidden)
            .background { AppBackground(style: style) }
    }
}

/// Empty or error state: the symbol on a soft glow, a short title, one
/// sentence, and a way forward when there is one.
struct EmptyStateView<Actions: View>: View {
    let symbol: String
    let title: LocalizedStringKey
    let message: LocalizedStringKey
    @ViewBuilder var actions: Actions

    @ScaledMetric(relativeTo: .largeTitle) private var badgeSize: CGFloat = 76

    var body: some View {
        VStack(spacing: Theme.Space.s4) {
            ZStack {
                Circle()
                    .fill(RadialGradient(colors: [Theme.accent.opacity(0.22), Theme.accent.opacity(0.04)], center: .center, startRadius: 0, endRadius: badgeSize * 0.7))
                Image(systemName: symbol)
                    .font(.system(size: badgeSize * 0.4, weight: .medium))
                    .foregroundStyle(Theme.accentText)
                    .symbolRenderingMode(.hierarchical)
            }
            .frame(width: badgeSize, height: badgeSize)
            .accessibilityHidden(true)

            VStack(spacing: Theme.Space.s2) {
                Text(title)
                    .font(.title3.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                Text(message)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .multilineTextAlignment(.center)

            VStack(spacing: Theme.Space.s2) {
                actions
            }
            .padding(.top, Theme.Space.s1)
        }
        .padding(.horizontal, Theme.Space.s8)
        .frame(maxWidth: 420)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

extension EmptyStateView where Actions == EmptyView {
    init(symbol: String, title: LocalizedStringKey, message: LocalizedStringKey) {
        self.init(symbol: symbol, title: title, message: message) { EmptyView() }
    }
}
