import Contacts
import Foundation
import HousephoneKit
import Observation
import os

struct ContactEntry: Identifiable, Hashable, Sendable {
    struct Number: Hashable, Sendable {
        let label: String?
        let value: String
    }

    let id: String
    let name: String
    let sortKey: String
    let numbers: [Number]
    /// The contact's small photo, if it has one.
    let thumbnail: Data?
}

/// Read-only view of the system contacts: a list for the contacts tab and
/// a number → name index for caller ID.
@MainActor
@Observable
final class ContactsDirectory {
    private(set) var authorization: CNAuthorizationStatus = CNContactStore.authorizationStatus(for: .contacts)
    private(set) var contacts: [ContactEntry] = []
    private(set) var isLoading = false

    @ObservationIgnored private let store = CNContactStore()
    @ObservationIgnored private var namesByNumber: [String: String] = [:]
    @ObservationIgnored private var entriesByNumber: [String: ContactEntry] = [:]
    @ObservationIgnored private var changeObserver: (any NSObjectProtocol)?
    /// Names for numbers that are not in the iPhone's contacts, e.g. from
    /// the FRITZ!Box phonebook.
    @ObservationIgnored var fallbackName: ((String) -> String?)?
    @ObservationIgnored private let logger = Logger(subsystem: "com.jorisconrad.housephone", category: "contacts")

    init() {
        changeObserver = NotificationCenter.default.addObserver(forName: .CNContactStoreDidChange, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.reload() }
        }
        reload()
    }

    var canRead: Bool {
        authorization == .authorized || authorization == .limited
    }

    func requestAccess() async {
        do {
            _ = try await store.requestAccess(for: .contacts)
        } catch {
            logger.error("Contacts access failed: \(error.localizedDescription, privacy: .public)")
        }
        authorization = CNContactStore.authorizationStatus(for: .contacts)
        reload()
    }

    func reload() {
        authorization = CNContactStore.authorizationStatus(for: .contacts)
        guard canRead else {
            contacts = []
            namesByNumber = [:]
            entriesByNumber = [:]
            return
        }
        isLoading = true
        Task {
            let loaded = await Self.fetchContacts()
            contacts = loaded
            var index: [String: String] = [:]
            var entries: [String: ContactEntry] = [:]
            for contact in loaded {
                for number in contact.numbers {
                    if let key = PhoneNumber.matchKey(number.value) {
                        index[key] = contact.name
                        entries[key] = contact
                    }
                }
            }
            namesByNumber = index
            entriesByNumber = entries
            isLoading = false
        }
    }

    /// Name of the contact with this number, if any: iPhone contacts first,
    /// then `fallbackName` (the FRITZ!Box phonebook). Synchronous so the
    /// VoIP push handler can use it before reporting to CallKit.
    func name(for number: String) -> String? {
        guard let key = PhoneNumber.matchKey(number) else { return nil }
        return namesByNumber[key] ?? fallbackName?(number)
    }

    /// The iPhone contact with this number, if any (no FRITZ!Box fallback).
    func contact(for number: String) -> ContactEntry? {
        guard let key = PhoneNumber.matchKey(number) else { return nil }
        return entriesByNumber[key]
    }

    /// Small photo of the contact with this number, for list rows.
    func thumbnail(for number: String) -> Data? {
        contact(for: number)?.thumbnail
    }

    /// The full-size photo, for the call screen. Loaded on demand: keeping
    /// every full photo in memory would be wasteful.
    func image(for number: String) async -> Data? {
        guard let contact = contact(for: number) else { return nil }
        let id = contact.id
        return await Self.fetchImage(contactID: id) ?? contact.thumbnail
    }

    @concurrent
    nonisolated private static func fetchImage(contactID: String) async -> Data? {
        let keys: [any CNKeyDescriptor] = [CNContactImageDataKey as CNKeyDescriptor]
        return try? CNContactStore().unifiedContact(withIdentifier: contactID, keysToFetch: keys).imageData
    }

    /// Runs off the main actor: enumerating a large address book takes a while.
    @concurrent
    nonisolated private static func fetchContacts() async -> [ContactEntry] {
        let store = CNContactStore()
        let keys: [any CNKeyDescriptor] = [
            CNContactFormatter.descriptorForRequiredKeys(for: .fullName),
            CNContactPhoneNumbersKey as CNKeyDescriptor,
            CNContactOrganizationNameKey as CNKeyDescriptor,
            CNContactThumbnailImageDataKey as CNKeyDescriptor,
        ]
        let request = CNContactFetchRequest(keysToFetch: keys)
        request.sortOrder = .userDefault

        var result: [ContactEntry] = []
        do {
            try store.enumerateContacts(with: request) { contact, _ in
                guard !contact.phoneNumbers.isEmpty else { return }
                let formatted = CNContactFormatter.string(from: contact, style: .fullName)
                let name = [formatted, contact.organizationName]
                    .compactMap { $0?.trimmingCharacters(in: .whitespaces) }
                    .first { !$0.isEmpty } ?? contact.phoneNumbers[0].value.stringValue
                let numbers = contact.phoneNumbers.map { labeled in
                    ContactEntry.Number(
                        label: labeled.label.map { CNLabeledValue<CNPhoneNumber>.localizedString(forLabel: $0) },
                        value: labeled.value.stringValue
                    )
                }
                result.append(ContactEntry(id: contact.identifier, name: name, sortKey: name.folding(options: [.diacriticInsensitive, .caseInsensitive], locale: .current), numbers: numbers, thumbnail: contact.thumbnailImageData))
            }
        } catch {
            return []
        }
        return result
    }
}
