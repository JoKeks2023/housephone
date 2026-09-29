import Foundation

/// An open-source component shipped inside the app.
///
/// All of them come in through Google's WebRTC framework (stasel/WebRTC
/// 153.0.0, verified against the binary). The watch app and HousephoneKit
/// use no third-party code. License texts live in `Resources/Licenses` and
/// were copied verbatim from the projects' official sources.
struct Acknowledgement: Identifiable {
    struct Link {
        let title: LocalizedStringResource
        let url: URL
    }

    /// Also the name of the bundled license file (`<id>.txt`), if any.
    let id: String
    let name: String
    let role: LocalizedStringResource
    /// License name as the project states it; not translated.
    let license: String
    let website: URL
    /// Further bundled files shown after the license, e.g. patent grants.
    var additionalFiles: [String] = []
    /// Texts that are only linked, not bundled.
    var links: [Link] = []

    /// The bundled license text, or `nil` if it is only linked.
    var licenseText: String? { Self.text(of: id) }

    var additionalTexts: [String] { additionalFiles.compactMap(Self.text(of:)) }

    private static func text(of file: String) -> String? {
        guard let url = Bundle.main.url(forResource: file, withExtension: "txt") else { return nil }
        return try? String(contentsOf: url, encoding: .utf8)
    }

    static let all: [Acknowledgement] = [
        Acknowledgement(
            id: "webrtc", name: "WebRTC", role: "Gesprächsverbindung und Audio",
            license: "BSD-3-Clause", website: URL(string: "https://webrtc.org")!,
            links: [Link(title: "Patentzusage", url: URL(string: "https://webrtc.googlesource.com/src/+/main/PATENTS")!)]
        ),
        Acknowledgement(
            id: "boringssl", name: "BoringSSL", role: "Verschlüsselung",
            license: "Apache-2.0", website: URL(string: "https://github.com/google/boringssl")!
        ),
        Acknowledgement(
            id: "libsrtp", name: "libSRTP", role: "Verschlüsselte Sprachübertragung",
            license: "BSD-3-Clause", website: URL(string: "https://github.com/cisco/libsrtp")!
        ),
        Acknowledgement(
            id: "opus", name: "Opus", role: "Audio-Codec",
            license: "BSD-3-Clause", website: URL(string: "https://opus-codec.org")!
        ),
        Acknowledgement(
            id: "abseil", name: "Abseil", role: "C++-Grundbibliothek",
            license: "Apache-2.0", website: URL(string: "https://abseil.io")!
        ),
        Acknowledgement(
            id: "protobuf", name: "Protocol Buffers", role: "Datenformat",
            license: "BSD-3-Clause", website: URL(string: "https://protobuf.dev")!
        ),
        Acknowledgement(
            id: "llvm-libcxxabi", name: "LLVM libc++abi", role: "C++-Laufzeit",
            license: "Apache-2.0 WITH LLVM-exception", website: URL(string: "https://libcxx.llvm.org")!
        ),
        Acknowledgement(
            id: "libvpx", name: "libvpx", role: "Video-Codecs VP8/VP9 (Teil von WebRTC)",
            license: "BSD-3-Clause", website: URL(string: "https://www.webmproject.org")!,
            additionalFiles: ["libvpx-patents"]
        ),
        Acknowledgement(
            id: "libaom", name: "libaom", role: "Video-Codec AV1 (Teil von WebRTC)",
            license: "BSD-2-Clause", website: URL(string: "https://aomedia.org")!
        ),
        Acknowledgement(
            id: "dav1d", name: "dav1d", role: "AV1-Decoder (Teil von WebRTC)",
            license: "BSD-2-Clause", website: URL(string: "https://code.videolan.org/videolan/dav1d")!
        ),
        Acknowledgement(
            id: "libyuv", name: "libyuv", role: "Bildformate (Teil von WebRTC)",
            license: "BSD-3-Clause", website: URL(string: "https://chromium.googlesource.com/libyuv/libyuv")!,
            links: [Link(title: "Lizenztext", url: URL(string: "https://chromium.googlesource.com/libyuv/libyuv/+/main/LICENSE")!)]
        ),
    ]
}
