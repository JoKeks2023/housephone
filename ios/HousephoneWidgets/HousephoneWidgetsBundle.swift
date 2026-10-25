import SwiftUI
import WidgetKit

@main
struct HousephoneWidgetsBundle: WidgetBundle {
    var body: some Widget {
        FavoritesWidget()
        RecentCallsWidget()
        MissedCallsWidget()
        if #available(iOS 18.0, *) {
            CallFavoriteControl()
            OpenKeypadControl()
        }
    }
}
