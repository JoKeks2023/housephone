import SwiftUI

/// One call in a call list (Housephone's own or the FRITZ!Box's): who,
/// direction and outcome, when. Tap calls back; the context menu offers
/// copy and "open in keypad". At accessibility text sizes the row stacks
/// instead of truncating.
struct CallHistoryRow: View {
    /// Contact or recorded name; `nil` shows the number.
    let name: String?
    let number: String
    let symbol: String
    /// Outcome or duration, e.g. "Verpasst" or "3:12".
    let detail: String
    let isMissed: Bool
    let date: Date
    let onCall: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(AppModel.self) private var appModel

    private var title: String {
        if let name { return name }
        return number.isEmpty ? String(localized: "Unbekannt") : number
    }

    private var subtitle: String {
        var parts: [String] = []
        if name != nil, !number.isEmpty { parts.append(number) }
        parts.append(detail)
        return parts.joined(separator: " · ")
    }

    var body: some View {
        let isAccessibilitySize = dynamicTypeSize.isAccessibilitySize
        let layout = isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: Theme.Space.s1))
            : AnyLayout(HStackLayout(spacing: Theme.Space.s3))

        Button(action: onCall) {
            layout {
                HStack(spacing: Theme.Space.s3) {
                    AvatarView(name: name, size: 40)
                    VStack(alignment: .leading, spacing: Theme.Space.hairline) {
                        Text(title)
                            .font(.body.weight(.medium))
                            .foregroundStyle(isMissed ? Theme.dangerText : Color.primary)
                            .lineLimit(isAccessibilitySize ? 3 : 1)
                        HStack(spacing: Theme.Space.s1) {
                            Image(systemName: symbol)
                                .imageScale(.small)
                                .accessibilityHidden(true)
                            Text(subtitle)
                        }
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(isAccessibilitySize ? 3 : 1)
                    }
                }
                if !isAccessibilitySize {
                    Spacer(minLength: Theme.Space.s2)
                }
                Text(CallDate.text(for: date))
                    .font(.subheadline)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(RowButtonStyle())
        .disabled(number.isEmpty)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel)
        .accessibilityAddTraits(.isButton)
        .accessibilityHint(number.isEmpty ? Text(verbatim: "") : Text("Zurückrufen"))
        .contextMenu {
            if !number.isEmpty {
                Button("Anrufen", systemImage: "phone", action: onCall)
                Button("Nummer kopieren", systemImage: "doc.on.doc") {
                    UIPasteboard.general.string = number
                }
                Button("Im Tastenfeld öffnen", systemImage: "circle.grid.3x3") {
                    appModel.keypadNumber = number
                    appModel.selectedTab = .keypad
                }
            }
        }
    }

    /// Name (or the number, read digit by digit), then the number, the
    /// outcome and the date.
    private var accessibilityLabel: Text {
        var label = name.map { AttributedString($0) }
            ?? (number.isEmpty ? AttributedString(String(localized: "Unbekannt")) : .spokenNumber(number))
        if name != nil, !number.isEmpty {
            label += AttributedString(", ") + .spokenNumber(number)
        }
        label += AttributedString(", \(detail), \(CallDate.text(for: date))")
        return Text(label)
    }
}
