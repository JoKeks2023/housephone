import HousephoneKit
import SwiftUI

/// Favorites as a row of people on top of the contacts, like the Phone
/// app: tap calls, the context menu removes. Without favorites a quiet
/// hint shows how to add one (where adding is possible).
struct FavoritesSection: View {
    var showsHint = true

    @Environment(FavoritesStore.self) private var favorites
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(CallCenter.self) private var callCenter

    var body: some View {
        if !favorites.favorites.isEmpty || showsHint {
            section
        }
    }

    private var section: some View {
        Section {
            if favorites.favorites.isEmpty {
                Label {
                    Text("Halte einen Kontakt gedrückt und wähle „Zu Favoriten“.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                } icon: {
                    Image(systemName: "star")
                        .foregroundStyle(Theme.accentText)
                }
                .listRowBackground(Color.clear)
            } else {
                ScrollView(.horizontal) {
                    LazyHStack(alignment: .top, spacing: Theme.Space.s4) {
                        ForEach(favorites.favorites) { favorite in
                            FavoriteTile(favorite: favorite, imageData: contacts.thumbnail(for: favorite.number)) {
                                let name = contacts.name(for: favorite.number) ?? favorite.name
                                Task { await callCenter.startCall(to: favorite.number, name: name) }
                            } onRemove: {
                                withMotion(Theme.Motion.standard) { favorites.remove(favorite) }
                            }
                        }
                    }
                    .padding(.horizontal, Theme.Space.s4)
                    .padding(.vertical, Theme.Space.s2)
                }
                .scrollIndicators(.hidden)
                .listRowInsets(EdgeInsets())
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
            }
        } header: {
            Text("Favoriten")
        }
    }
}

private struct FavoriteTile: View {
    let favorite: Favorite
    let imageData: Data?
    let onCall: () -> Void
    let onRemove: () -> Void

    @ScaledMetric(relativeTo: .body) private var avatarSize: CGFloat = 60

    /// First name only, like the Phone app's favorites.
    private var shortName: String {
        favorite.name.split(separator: " ").first.map(String.init) ?? favorite.name
    }

    var body: some View {
        Button(action: onCall) {
            VStack(spacing: Theme.Space.s1) {
                AvatarView(name: favorite.name, imageData: imageData, size: avatarSize)
                Text(shortName)
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                Text(favorite.label ?? String(localized: "Telefon"))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            .frame(width: avatarSize + Theme.Space.s4)
            .contentShape(Rectangle())
        }
        .buttonStyle(PressableButtonStyle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: [favorite.name, favorite.label].compactMap(\.self).joined(separator: ", ")))
        .accessibilityHint(Text("Anrufen"))
        .accessibilityAction(named: Text("Aus Favoriten entfernen"), onRemove)
        .contextMenu {
            Button("Anrufen", systemImage: "phone", action: onCall)
            Button("Nummer kopieren", systemImage: "doc.on.doc") {
                UIPasteboard.general.string = favorite.number
            }
            Button("Aus Favoriten entfernen", systemImage: "star.slash", role: .destructive, action: onRemove)
        }
    }
}
