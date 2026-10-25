import SwiftUI

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
