import SwiftUI
import WidgetKit

@main
struct HousephoneWatchWidgetsBundle: WidgetBundle {
    var body: some Widget {
        FavoriteComplication()
        MissedCallsComplication()
    }
}
