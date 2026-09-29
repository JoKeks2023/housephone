import Foundation

public enum PhoneNumber {
    /// Characters the bridge accepts in `call.dial` and `call.dtmf`.
    public static let dialCharacters = Set("0123456789*#+")

    /// Removes formatting (spaces, dashes, brackets, slashes, dots) and
    /// returns a number the bridge accepts: digits, `*`, `#` and at most one
    /// leading `+`. Returns `nil` for anything else, e.g. letters.
    public static func dialable(_ input: String) -> String? {
        var result = ""
        for character in input {
            switch character {
            case "0"..."9", "*", "#":
                result.append(character)
            case "+":
                guard result.isEmpty else { return nil }
                result.append(character)
            case _ where character.isWhitespace:
                continue
            case "-", "(", ")", "/", ".", "\u{2010}", "\u{2011}", "\u{2012}", "\u{2013}":
                continue
            default:
                return nil
            }
        }
        return result.isEmpty || result == "+" ? nil : result
    }

    /// Key for recognizing one number written in different ways, e.g.
    /// `+49 30 123456`, `0049 30 123456` and `030 123456` all become
    /// `030123456`. Numbers from other countries keep a `00` prefix.
    public static func matchKey(_ input: String, countryCode: String = "49") -> String? {
        guard let dialable = dialable(input) else { return nil }
        var digits = dialable
        if digits.hasPrefix("+") {
            digits.removeFirst()
            digits = "00" + digits
        }
        guard digits.allSatisfy(\.isNumber) else { return nil }
        let international = "00" + countryCode
        if digits.hasPrefix(international) {
            digits = "0" + digits.dropFirst(international.count)
        }
        return digits.isEmpty ? nil : digits
    }

    /// DTMF digits the bridge accepts: `0-9 * #`.
    public static func isDTMF(_ digits: String) -> Bool {
        !digits.isEmpty && digits.allSatisfy { $0.isNumber || $0 == "*" || $0 == "#" }
    }
}
