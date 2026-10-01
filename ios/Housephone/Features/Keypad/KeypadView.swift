import HousephoneKit
import SwiftData
import SwiftUI

struct KeypadView: View {
    @Environment(AppModel.self) private var appModel
    @Environment(CallCenter.self) private var callCenter
    @Environment(ContactsDirectory.self) private var contacts
    @Environment(FavoritesStore.self) private var favorites

    @Query(
        filter: #Predicate<CallRecord> { $0.directionRaw == "outgoing" },
        sort: \CallRecord.date,
        order: .reverse
    )
    private var outgoingCalls: [CallRecord]

    @State private var clearTrigger = 0

    /// The fixed part of the layout below: status card with top padding
    /// up to 64 (it grows with Dynamic Type), two spacers 2 × 24,
    /// number display 82, grid row spacing 3 × 16, call row padding
    /// 16 + 32. The rest scales with the key size: four key rows plus the
    /// call row = 5 keys. Per-device numbers are in the T-0007 report.
    private static let fixedHeight: CGFloat = 290

    /// Keys shrink on short screens (iPhone SE) instead of pushing the
    /// call button under the tab bar; 78 pt is the Phone app size.
    private static func keySize(for height: CGFloat) -> CGFloat {
        min(78, max(56, (height - fixedHeight) / 5))
    }

    var body: some View {
        @Bindable var appModel = appModel
        let number = appModel.keypadNumber

        GeometryReader { proxy in
            let keySize = Self.keySize(for: proxy.size.height)
            VStack(spacing: 0) {
                ConnectionStatusCard()
                    .padding(.horizontal, Theme.Space.s4)
                    .padding(.top, Theme.Space.s2)

                Spacer(minLength: Theme.Space.s6)

                NumberDisplay(
                    number: number,
                    contactName: contacts.name(for: number),
                    suggestion: suggestion(for: number)
                ) { pasted in
                    appModel.keypadNumber = pasted
                } onSuggestion: { suggested in
                    appModel.keypadNumber = suggested.number
                }
                .padding(.horizontal, Theme.Space.s6)

                Spacer(minLength: Theme.Space.s6)

                KeypadGrid(keySize: keySize) { key in
                    appModel.keypadNumber.append(key)
                }

                HStack {
                    Color.clear.frame(width: keySize, height: keySize)
                        .frame(maxWidth: .infinity)

                    CallActionButton(kind: .start, label: "Anrufen", size: min(76, keySize)) {
                        call()
                    }
                    .frame(maxWidth: .infinity)

                    DeleteKey(isVisible: !number.isEmpty, size: keySize) {
                        guard !appModel.keypadNumber.isEmpty else { return }
                        appModel.keypadNumber.removeLast()
                    } onClear: {
                        appModel.keypadNumber = ""
                        clearTrigger += 1
                    }
                    .frame(maxWidth: .infinity)
                }
                .padding(.horizontal, Theme.Space.s8)
                .padding(.top, Theme.Space.s4)
                .padding(.bottom, Theme.Space.s8)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background { AppBackground() }
        .sensoryFeedback(.impact(weight: .medium), trigger: clearTrigger)
    }

    /// Like the Phone app: while typing, the best match from contacts,
    /// favorites and recent calls, with how many more there are.
    private func suggestion(for number: String) -> KeypadSuggestion? {
        let digits = number.filter(\.isNumber)
        guard digits.count >= 3, contacts.name(for: number) == nil else { return nil }

        var matches: [KeypadSuggestion] = []
        var seen = Set<String>()
        func consider(name: String, number: String, label: String?, imageData: Data?) {
            let key = PhoneNumber.matchKey(number) ?? number
            guard !seen.contains(key), number.filter(\.isNumber).contains(digits) else { return }
            seen.insert(key)
            matches.append(KeypadSuggestion(name: name, number: number, label: label, imageData: imageData, more: 0))
        }
        for favorite in favorites.favorites {
            consider(name: favorite.name, number: favorite.number, label: favorite.label, imageData: contacts.thumbnail(for: favorite.number))
        }
        for record in outgoingCalls.prefix(50) {
            if let name = contacts.name(for: record.number) ?? record.name, !name.isEmpty {
                consider(name: name, number: record.number, label: nil, imageData: contacts.thumbnail(for: record.number))
            }
        }
        for contact in contacts.contacts {
            for entry in contact.numbers {
                consider(name: contact.name, number: entry.value, label: entry.label, imageData: contact.thumbnail)
            }
        }
        guard var best = matches.first else { return nil }
        best.more = matches.count - 1
        return best
    }

    private func call() {
        let number = appModel.keypadNumber
        guard !number.isEmpty else {
            // Like the Phone app: an empty call button recalls the last number.
            if let last = outgoingCalls.first?.number { appModel.keypadNumber = last }
            return
        }
        Task {
            if await callCenter.startCall(to: number, name: contacts.name(for: number)) {
                appModel.keypadNumber = ""
            }
        }
    }
}

/// A number from contacts, favorites or recents that contains the typed
/// digits.
struct KeypadSuggestion: Equatable {
    let name: String
    let number: String
    let label: String?
    let imageData: Data?
    /// How many other entries match too.
    var more: Int
}

private struct NumberDisplay: View {
    let number: String
    let contactName: String?
    let suggestion: KeypadSuggestion?
    let onPaste: (String) -> Void
    let onSuggestion: (KeypadSuggestion) -> Void

    /// 38 pt at the default size, growing and shrinking with Dynamic Type.
    @ScaledMetric(relativeTo: .largeTitle) private var numberSize: CGFloat = 38

    var body: some View {
        VStack(spacing: Theme.Space.s2) {
            // No numeric-text roll while typing: dozens of times a day,
            // the digit should just be there.
            Text(number.isEmpty ? " " : number)
                .font(.system(size: numberSize, weight: .regular))
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.45)
                .truncationMode(.head)
                .accessibilityLabel(number.isEmpty ? Text("Keine Nummer eingegeben") : Text.spokenNumber(number))

            ZStack {
                if let contactName {
                    Text(contactName)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Theme.accentText)
                        .transition(.opacity)
                } else if let suggestion {
                    SuggestionButton(suggestion: suggestion) { onSuggestion(suggestion) }
                        .transition(.opacity.combined(with: .scale(scale: 0.97)))
                } else if number.isEmpty {
                    PasteButton(payloadType: String.self) { strings in
                        guard let text = strings.first, let dialable = PhoneNumber.dialable(text) else { return }
                        onPaste(dialable)
                    }
                    .labelStyle(.titleAndIcon)
                    .buttonBorderShape(.capsule)
                    .controlSize(.small)
                    .tint(.secondary)
                    .transition(.opacity)
                }
            }
            .frame(minHeight: 36)
            .motion(Theme.Motion.standard, value: contactName)
            .motion(Theme.Motion.snappy, value: suggestion?.number)
        }
        .frame(maxWidth: .infinity)
    }
}

/// The match under the number: tap to take its number.
private struct SuggestionButton: View {
    let suggestion: KeypadSuggestion
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.s2) {
                AvatarView(name: suggestion.name, imageData: suggestion.imageData, size: 24)
                Text(suggestion.name)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Theme.accentText)
                    .lineLimit(1)
                Text(suggestion.label ?? suggestion.number)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                if suggestion.more > 0 {
                    Text(verbatim: "+\(suggestion.more)")
                        .font(.caption.weight(.medium))
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.vertical, Theme.Space.s1)
            .padding(.leading, Theme.Space.s1)
            .padding(.trailing, Theme.Space.s3)
            .glassEffect(.regular.interactive(), in: .capsule)
        }
        .buttonStyle(PressableButtonStyle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: "\(suggestion.name), \(suggestion.label ?? suggestion.number)"))
        .accessibilityHint(Text("Übernimmt diese Nummer"))
    }
}

/// Deletes the last digit; holding it clears the number.
private struct DeleteKey: View {
    let isVisible: Bool
    let size: CGFloat
    let onDelete: () -> Void
    let onClear: () -> Void

    var body: some View {
        Button(action: onDelete) {
            Image(systemName: "delete.left.fill")
                .font(.title2)
                .foregroundStyle(.secondary)
                .frame(width: size, height: size)
                .contentShape(Rectangle())
        }
        .buttonStyle(PressableButtonStyle())
        .simultaneousGesture(LongPressGesture(minimumDuration: 0.6).onEnded { _ in onClear() })
        .opacity(isVisible ? 1 : 0)
        .disabled(!isVisible)
        .motion(Theme.Motion.snappy, value: isVisible)
        .accessibilityLabel(Text("Löschen"))
        .accessibilityHint(Text("Gedrückt halten löscht die ganze Nummer"))
        .accessibilityAction(named: Text("Nummer löschen")) { onClear() }
    }
}
