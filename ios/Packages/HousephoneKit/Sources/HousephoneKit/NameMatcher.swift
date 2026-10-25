import Foundation

/// Finds people by a spoken or typed name, e.g. for Siri ("ruf Oma an") or
/// a search field. Ignores case and diacritics.
public enum NameMatcher {
    /// Candidates whose name matches `query`, best matches first:
    /// 1. the whole name equals the query,
    /// 2. every word of the query starts a word of the name
    ///    ("anna m" finds "Anna Müller").
    /// Within a rank the input order is kept. Empty for an empty query.
    public static func matches<Candidate>(_ query: String, in candidates: [Candidate], name: (Candidate) -> String) -> [Candidate] {
        let queryWords = words(query)
        guard !queryWords.isEmpty else { return [] }
        let normalizedQuery = queryWords.joined(separator: " ")

        var exact: [Candidate] = []
        var prefix: [Candidate] = []
        for candidate in candidates {
            let nameWords = words(name(candidate))
            if nameWords.joined(separator: " ") == normalizedQuery {
                exact.append(candidate)
            } else if queryWords.allSatisfy({ word in nameWords.contains { $0.hasPrefix(word) } }) {
                prefix.append(candidate)
            }
        }
        return exact.isEmpty ? prefix : exact
    }

    static func words(_ text: String) -> [String] {
        text.folding(options: [.caseInsensitive, .diacriticInsensitive, .widthInsensitive], locale: Locale(identifier: "de_DE"))
            .split { !$0.isLetter && !$0.isNumber }
            .map(String.init)
    }
}
