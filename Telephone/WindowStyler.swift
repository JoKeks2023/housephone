//
//  WindowStyler.swift
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

/// Provides modern styling for windows on macOS 13+
@objc class WindowStyler: NSObject {
    
    /// Apply modern styling to a window
    @objc static func applyModernStyle(to window: NSWindow) {
        // Use unified title and toolbar style for a cleaner look
        if #available(macOS 11.0, *) {
            window.titlebarSeparatorStyle = .none
        }
        
        // Enable full size content view for a more modern appearance
        window.styleMask.insert(.fullSizeContentView)
        
        // Use a vibrant window background
        if #available(macOS 10.14, *) {
            window.contentView?.wantsLayer = true
            
            // Apply modern window appearance
            let visualEffect = NSVisualEffectView()
            visualEffect.material = .sidebar
            visualEffect.blendingMode = .behindWindow
            visualEffect.state = .followsWindowActiveState
            
            if let contentView = window.contentView {
                visualEffect.frame = contentView.bounds
                visualEffect.autoresizingMask = [.width, .height]
                
                // Insert visual effect behind all other views
                contentView.addSubview(visualEffect, positioned: .below, relativeTo: contentView.subviews.first)
            }
        }
    }
    
    /// Apply subtle shadows and corner radius to improve appearance
    @objc static func applyModernViewStyle(to view: NSView) {
        view.wantsLayer = true
        
        if let layer = view.layer {
            layer.cornerRadius = 6.0
            layer.masksToBounds = true
            
            // Add subtle shadow for depth
            if #available(macOS 10.14, *) {
                layer.shadowColor = NSColor.shadowColor.cgColor
                layer.shadowOpacity = 0.1
                layer.shadowOffset = CGSize(width: 0, height: -1)
                layer.shadowRadius = 3.0
            }
        }
    }
}
