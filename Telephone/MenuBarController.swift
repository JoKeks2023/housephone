//
//  MenuBarController.swift
//  Telephone
//
//  Copyright © 2016-2022 64 Characters
//
//  Telephone is free software: you can redistribute it and/or modify
//  it under the terms of the GNU General Public License as published by
//  the Free Software Foundation, either version 3 of the License, or
//  (at your option) any later version.
//
//  Telephone is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//  GNU General Public License for more details.
//

import Cocoa

@objc protocol MenuBarControllerDelegate: AnyObject {
    func menuBarControllerDidRequestShowWindow()
    func menuBarControllerDidRequestShowPreferences()
    func menuBarControllerDidRequestMakeCall()
    func menuBarControllerDidRequestQuit()
}

@objc class MenuBarController: NSObject {
    private var statusItem: NSStatusItem?
    private let menu = NSMenu()
    
    @objc weak var delegate: MenuBarControllerDelegate?
    @objc var isEnabled: Bool = false {
        didSet {
            if isEnabled {
                setupStatusItem()
            } else {
                removeStatusItem()
            }
        }
    }
    
    @objc var accountsMenuItems: AccountsMenuItems? {
        didSet {
            updateMenu()
        }
    }
    
    override init() {
        super.init()
        setupMenu()
    }
    
    private func setupStatusItem() {
        guard statusItem == nil else { return }
        
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        
        if let button = statusItem?.button {
            // Use SF Symbol for modern macOS look
            if #available(macOS 11.0, *) {
                button.image = NSImage(systemSymbolName: "phone.fill", accessibilityDescription: "Telephone")
            } else {
                button.image = NSImage(named: "AppIcon")
            }
            button.image?.isTemplate = true
        }
        
        statusItem?.menu = menu
    }
    
    private func removeStatusItem() {
        if let statusItem = statusItem {
            NSStatusBar.system.removeStatusItem(statusItem)
            self.statusItem = nil
        }
    }
    
    private func setupMenu() {
        updateMenu()
    }
    
    @objc func updateMenu() {
        menu.removeAllItems()
        
        // Show Window item
        let showWindowItem = NSMenuItem(
            title: NSLocalizedString("Show Window", comment: "Menu bar show window"),
            action: #selector(showWindowAction(_:)),
            keyEquivalent: ""
        )
        showWindowItem.target = self
        menu.addItem(showWindowItem)
        
        menu.addItem(NSMenuItem.separator())
        
        // Make Call item
        let makeCallItem = NSMenuItem(
            title: NSLocalizedString("Make Call…", comment: "Menu bar make call"),
            action: #selector(makeCallAction(_:)),
            keyEquivalent: ""
        )
        makeCallItem.target = self
        menu.addItem(makeCallItem)
        
        // Add account windows submenu if available
        if let accountsMenuItems = accountsMenuItems {
            menu.addItem(NSMenuItem.separator())
            
            let accountsSubmenu = NSMenu(title: "Accounts")
            let accountsMenuItem = NSMenuItem(title: NSLocalizedString("Accounts", comment: "Menu bar accounts"), action: nil, keyEquivalent: "")
            accountsMenuItem.submenu = accountsSubmenu
            
            // Add account items from existing menu
            for item in accountsMenuItems.items {
                if let itemCopy = item.copy() as? NSMenuItem {
                    accountsSubmenu.addItem(itemCopy)
                }
            }
            
            menu.addItem(accountsMenuItem)
        }
        
        menu.addItem(NSMenuItem.separator())
        
        // Preferences item
        let preferencesItem = NSMenuItem(
            title: NSLocalizedString("Preferences…", comment: "Menu bar preferences"),
            action: #selector(preferencesAction(_:)),
            keyEquivalent: ","
        )
        preferencesItem.target = self
        menu.addItem(preferencesItem)
        
        menu.addItem(NSMenuItem.separator())
        
        // Quit item
        let quitItem = NSMenuItem(
            title: NSLocalizedString("Quit Telephone", comment: "Menu bar quit"),
            action: #selector(quitAction(_:)),
            keyEquivalent: "q"
        )
        quitItem.target = self
        menu.addItem(quitItem)
    }
    
    @objc func updateBadge(count: Int) {
        guard let button = statusItem?.button else { return }
        
        if count > 0 {
            // Update accessibility for VoiceOver
            button.image?.accessibilityDescription = "Telephone (\(count) unhandled call\(count == 1 ? "" : "s"))"
            
            // Add visual badge by modifying the button's attributed title
            // This displays the count next to the icon
            let attributes: [NSAttributedString.Key: Any] = [
                .font: NSFont.systemFont(ofSize: 12, weight: .medium),
                .foregroundColor: NSColor.controlAccentColor
            ]
            button.attributedTitle = NSAttributedString(string: " \(count)", attributes: attributes)
        } else {
            button.image?.accessibilityDescription = "Telephone"
            button.attributedTitle = NSAttributedString(string: "")
        }
    }
    
    // MARK: - Actions
    
    @objc private func showWindowAction(_ sender: Any) {
        delegate?.menuBarControllerDidRequestShowWindow()
    }
    
    @objc private func makeCallAction(_ sender: Any) {
        delegate?.menuBarControllerDidRequestMakeCall()
    }
    
    @objc private func preferencesAction(_ sender: Any) {
        delegate?.menuBarControllerDidRequestShowPreferences()
    }
    
    @objc private func quitAction(_ sender: Any) {
        delegate?.menuBarControllerDidRequestQuit()
    }
}
