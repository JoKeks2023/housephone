import SwiftUI

/// Settings → Über → Danksagungen: the open-source components inside the
/// app and their licenses.
struct AcknowledgementsView: View {
    var body: some View {
        List {
            Section {
                ForEach(Acknowledgement.all) { item in
                    NavigationLink {
                        LicenseView(item: item)
                    } label: {
                        VStack(alignment: .leading, spacing: Theme.Space.s1) {
                            Text(item.name)
                                .font(.body.weight(.medium))
                            Text(item.role)
                                .font(.subheadline)
                                .foregroundStyle(Theme.textSecondary)
                            Text(item.license)
                                .font(.caption.monospaced())
                                .foregroundStyle(Theme.textSecondary)
                        }
                        .padding(.vertical, Theme.Space.s1)
                        .accessibilityElement(children: .combine)
                    }
                }
            } header: {
                Text("Open Source")
            } footer: {
                Text("Housephone telefoniert mit WebRTC von Google, und darin stecken diese Open-Source-Projekte. Danke an alle, die daran mitarbeiten. Die App für die Apple Watch nutzt keine fremden Bibliotheken.")
            }
        }
        .navigationTitle("Danksagungen")
        .navigationBarTitleDisplayMode(.inline)
    }
}

private struct LicenseView: View {
    let item: Acknowledgement

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.s4) {
                VStack(alignment: .leading, spacing: Theme.Space.s1) {
                    Text(item.role)
                        .font(.subheadline)
                        .foregroundStyle(Theme.textSecondary)
                    Text(item.license)
                        .font(.callout.monospaced())
                }

                Link(destination: item.website) {
                    Label(item.website.host() ?? item.website.absoluteString, systemImage: "safari")
                }
                .font(.callout)

                if let text = item.licenseText {
                    licenseBlock(text)
                }
                ForEach(Array(item.additionalTexts.enumerated()), id: \.offset) { _, text in
                    licenseBlock(text)
                }
                if !item.links.isEmpty {
                    VStack(alignment: .leading, spacing: Theme.Space.s2) {
                        if item.licenseText == nil {
                            Text("Den Lizenztext findest du online:")
                                .font(.callout)
                                .foregroundStyle(Theme.textSecondary)
                        }
                        ForEach(item.links, id: \.url) { link in
                            Link(destination: link.url) {
                                Label(String(localized: link.title), systemImage: "doc.text")
                            }
                            .font(.callout)
                        }
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(Theme.Space.s4)
        }
        .navigationTitle(item.name)
        .navigationBarTitleDisplayMode(.inline)
    }

    private func licenseBlock(_ text: String) -> some View {
        Text(text.trimmingCharacters(in: .whitespacesAndNewlines))
            .font(.footnote.monospaced())
            .foregroundStyle(Theme.textPrimary)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(Theme.Space.s3)
            .background(.fill.quaternary, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
    }
}
