import CarPlay
import HousephoneKit
import Observation
import SwiftUI
import UIKit

/// Housephone in the car: favorites, recent calls and the FRITZ!Box
/// phonebook as lists. A tap calls through CallKit; the call itself shows in
/// CarPlay's own call screen, as do incoming calls.
///
/// Runs only with the CarPlay communication entitlement, which Apple grants
/// on request (ios/README.md). Without it the scene never connects.
@MainActor
final class CarPlayInterface {
    private let interfaceController: CPInterfaceController
    private let services: AppServices
    private let favoritesTemplate: CPListTemplate
    private let recentsTemplate: CPListTemplate
    private let contactsTemplate: CPListTemplate
    private var isConnected = true

    init(interfaceController: CPInterfaceController, services: AppServices = .shared) {
        self.interfaceController = interfaceController
        self.services = services
        favoritesTemplate = Self.tab(title: String(localized: "Favoriten"), symbol: "star.fill")
        recentsTemplate = Self.tab(title: String(localized: "Anrufe"), symbol: "clock.fill")
        contactsTemplate = Self.tab(title: String(localized: "Kontakte"), symbol: "person.crop.circle.fill")
        let tabs = CPTabBarTemplate(templates: [favoritesTemplate, recentsTemplate, contactsTemplate])
        interfaceController.setRootTemplate(tabs, animated: false, completion: nil)
        update()
        observe()
    }

    func disconnect() {
        isConnected = false
    }

    private static func tab(title: String, symbol: String) -> CPListTemplate {
        let template = CPListTemplate(title: title, sections: [])
        template.tabTitle = title
        template.tabImage = UIImage(systemName: symbol)
        return template
    }

    /// Observation reports one change per registration, so this registers
    /// again after each one, until CarPlay disconnects.
    private func observe() {
        withObservationTracking {
            _ = services.snapshot.latest
            _ = services.fritzBox.phonebook.value
            _ = services.bridge.isPaired
            _ = services.direct.isEnabled
        } onChange: { [self] in
            Task { @MainActor in
                guard self.isConnected else { return }
                self.update()
                self.observe()
            }
        }
    }

    // MARK: - Lists

    private func update() {
        guard services.bridge.isPaired || services.direct.isEnabled else {
            for template in [favoritesTemplate, recentsTemplate, contactsTemplate] {
                template.updateSections([])
                template.emptyViewTitleVariants = [String(localized: "Housephone ist nicht eingerichtet")]
                template.emptyViewSubtitleVariants = [String(localized: "Richte Housephone auf dem iPhone ein.")]
            }
            return
        }
        let snapshot = services.snapshot.latest ?? .empty
        let limit = Int(CPListTemplate.maximumItemCount)

        favoritesTemplate.emptyViewTitleVariants = [String(localized: "Keine Favoriten")]
        favoritesTemplate.emptyViewSubtitleVariants = [String(localized: "Lege Favoriten in Housephone auf dem iPhone an.")]
        favoritesTemplate.updateSections([CPListSection(items: snapshot.favorites.prefix(limit).map(favoriteItem))])

        recentsTemplate.emptyViewTitleVariants = [String(localized: "Noch keine Anrufe")]
        recentsTemplate.emptyViewSubtitleVariants = []
        recentsTemplate.updateSections([CPListSection(items: snapshot.recentCalls.prefix(limit).map(recentItem))])

        contactsTemplate.emptyViewTitleVariants = [String(localized: "Kein FRITZ!Box-Telefonbuch")]
        contactsTemplate.emptyViewSubtitleVariants = [String(localized: "Das Telefonbuch erscheint, sobald die App es einmal geladen hat.")]
        contactsTemplate.updateSections(contactSections(limit: limit))
    }

    private func favoriteItem(_ favorite: SharedSnapshot.Favorite) -> CPListItem {
        let item = CPListItem(text: favorite.name, detailText: favorite.label ?? favorite.number, image: Self.avatar(for: favorite.name))
        setCall(on: item, number: favorite.number, name: favorite.name)
        return item
    }

    private func recentItem(_ call: SharedSnapshot.RecentCall) -> CPListItem {
        let title = call.name.flatMap { $0.isEmpty ? nil : $0 } ?? (call.number.isEmpty ? String(localized: "Unbekannt") : call.number)
        let kind = call.isMissed
            ? String(localized: "Verpasst")
            : (call.direction == .outgoing ? String(localized: "Ausgehend") : String(localized: "Eingehend"))
        let symbol = call.direction == .outgoing ? "phone.arrow.up.right" : "phone.arrow.down.left"
        var image = UIImage(systemName: symbol)
        if call.isMissed {
            image = image?.withTintColor(.systemRed, renderingMode: .alwaysOriginal)
        }
        let item = CPListItem(text: title, detailText: "\(kind) · \(Self.time(call.date))", image: image)
        if PhoneNumber.dialable(call.number) != nil {
            setCall(on: item, number: call.number, name: call.name)
        } else {
            item.isEnabled = false
        }
        return item
    }

    /// The phonebook in alphabetical sections, within CarPlay's limits.
    private func contactSections(limit: Int) -> [CPListSection] {
        let contacts = (services.fritzBox.phonebook.value?.contacts ?? [])
            .filter { !$0.numbers.isEmpty }
            .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
            .prefix(limit)
        var sections: [(letter: String, items: [CPListItem])] = []
        for contact in contacts {
            let letter = Self.indexLetter(for: contact.name)
            let item = contactItem(contact)
            if sections.last?.letter == letter {
                sections[sections.count - 1].items.append(item)
            } else {
                sections.append((letter, [item]))
            }
        }
        return sections.prefix(Int(CPListTemplate.maximumSectionCount)).map {
            CPListSection(items: $0.items, header: $0.letter, sectionIndexTitle: $0.letter)
        }
    }

    private func contactItem(_ contact: FritzBoxContact) -> CPListItem {
        let single = contact.numbers.count == 1 ? contact.numbers.first : nil
        let detail = single.map { String(localized: $0.type.label) } ?? String(localized: "\(contact.numbers.count) Nummern")
        let item = CPListItem(text: contact.name, detailText: detail, image: Self.avatar(for: contact.name))
        if let single {
            setCall(on: item, number: single.number, name: contact.name)
        } else {
            item.accessoryType = .disclosureIndicator
            item.handler = { [weak self] _, completion in
                MainActor.assumeIsolated { self?.showNumbers(of: contact) }
                completion()
            }
        }
        return item
    }

    private func showNumbers(of contact: FritzBoxContact) {
        let items = contact.numbers.map { number in
            let item = CPListItem(text: String(localized: number.type.label), detailText: number.number)
            setCall(on: item, number: number.number, name: contact.name)
            return item
        }
        let template = CPListTemplate(title: contact.name, sections: [CPListSection(items: items)])
        interfaceController.pushTemplate(template, animated: true, completion: nil)
    }

    // MARK: - Calling

    private func setCall(on item: CPListItem, number: String, name: String?) {
        item.handler = { [weak self] _, completion in
            MainActor.assumeIsolated { self?.call(number, name: name) }
            completion()
        }
    }

    private func call(_ number: String, name: String?) {
        Task {
            guard await !services.callCenter.startCall(to: number, name: name) else { return }
            let message = services.callCenter.failure.map { String(localized: $0.message) }
                ?? String(localized: "Der Anruf konnte nicht gestartet werden.")
            showAlert(message)
        }
    }

    private func showAlert(_ message: String) {
        let ok = CPAlertAction(title: String(localized: "OK"), style: .cancel) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.interfaceController.dismissTemplate(animated: true, completion: nil)
            }
        }
        let alert = CPAlertTemplate(titleVariants: [message], actions: [ok])
        interfaceController.presentTemplate(alert, animated: true, completion: nil)
    }

    // MARK: - Formatting

    private static func time(_ date: Date) -> String {
        Calendar.current.isDateInToday(date)
            ? date.formatted(date: .omitted, time: .shortened)
            : date.formatted(.dateTime.day().month(.abbreviated))
    }

    private static func indexLetter(for name: String) -> String {
        guard let first = name.folding(options: [.diacriticInsensitive, .caseInsensitive], locale: .current).first,
              first.isLetter
        else { return "#" }
        return String(first).uppercased()
    }

    /// Initials on the person's identity color, as in the app.
    private static func avatar(for name: String) -> UIImage? {
        guard let initials = Monogram.initials(for: name), let tint = AvatarTint.forName(name) else {
            return UIImage(systemName: "person.crop.circle.fill")
        }
        let size = CPListItem.maximumImageSize
        let side = min(size.width, size.height)
        return UIGraphicsImageRenderer(size: CGSize(width: side, height: side)).image { _ in
            UIColor(tint.color).setFill()
            UIBezierPath(ovalIn: CGRect(x: 0, y: 0, width: side, height: side)).fill()
            let text = initials as NSString
            let font = UIFont.systemFont(ofSize: side * 0.38, weight: .semibold)
            let attributes: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: UIColor.white]
            let textSize = text.size(withAttributes: attributes)
            text.draw(at: CGPoint(x: (side - textSize.width) / 2, y: (side - textSize.height) / 2), withAttributes: attributes)
        }
    }
}

/// Connects the CarPlay scene (`Info.plist`, `AppDelegate`).
final class CarPlaySceneDelegate: UIResponder, CPTemplateApplicationSceneDelegate {
    private var interface: CarPlayInterface?

    func templateApplicationScene(_ templateApplicationScene: CPTemplateApplicationScene, didConnect interfaceController: CPInterfaceController) {
        interface = CarPlayInterface(interfaceController: interfaceController)
    }

    func templateApplicationScene(_ templateApplicationScene: CPTemplateApplicationScene, didDisconnectInterfaceController interfaceController: CPInterfaceController) {
        interface?.disconnect()
        interface = nil
    }
}
