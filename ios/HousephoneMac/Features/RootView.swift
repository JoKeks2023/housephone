import HousephoneKit
import SwiftUI

/// First slice of the Mac app (T-0018): proves that the shared package runs
/// on macOS by reading a pairing link. Pairing, calls and the call UI follow.
struct RootView: View {
    @State private var text = ""
    @State private var link: PairingLink?
    @State private var error: PairingLinkError?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Housephone")
                .font(.largeTitle.bold())
            Text("Füge den Kopplungslink deiner Bridge ein.")
                .foregroundStyle(.secondary)

            TextField("housephone://pair?…", text: $text)
                .textFieldStyle(.roundedBorder)
                .onSubmit(read)

            Button("Link prüfen", action: read)
                .keyboardShortcut(.defaultAction)
                .disabled(text.isEmpty)

            if let link {
                Label(link.bridgeName ?? link.bridgeURL.host() ?? link.bridgeURL.absoluteString, systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.green)
            } else if let error {
                Label(String(describing: error), systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.red)
            }
            Spacer()
        }
        .padding(24)
    }

    private func read() {
        do {
            link = try PairingLink(string: text)
            error = nil
        } catch let failure as PairingLinkError {
            link = nil
            error = failure
        } catch {
            link = nil
        }
    }
}
