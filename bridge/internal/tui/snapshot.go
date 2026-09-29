package tui

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

// Snapshot prints the overview and the self-test once, for use without a
// terminal (`./housephone tui | less`, scripts, CI).
func Snapshot(api API, w io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st, err := api.Status(ctx)
	if err != nil {
		return err
	}
	checks, err := api.SelfTest(ctx)
	if err != nil {
		return err
	}
	m := New(api, time.Now)
	m.width, m.status = 80, st
	fmt.Fprintf(w, "Housephone Bridge · %s\n\n%s\nSelbsttest: %s\n", st.BridgeName, m.viewOverview(), admin.Worst(checks))
	for _, c := range checks {
		fmt.Fprintf(w, "  [%-4s] %-24s %s\n", c.State, c.Name, c.Detail)
		if c.Hint != "" && c.State != admin.CheckOK {
			fmt.Fprintf(w, "         → %s\n", c.Hint)
		}
	}
	return nil
}
