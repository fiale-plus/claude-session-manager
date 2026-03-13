package tui

// renderHints renders the keyboard hints bar.
func renderHints(queueVisible bool, hasPending bool, width int) string {
	keys := []struct {
		key  string
		desc string
	}{
		{"\u2190\u2192", "navigate"},
		{"a", "autopilot"},
	}

	if hasPending {
		keys = append(keys,
			struct{ key, desc string }{"y", "approve"},
			struct{ key, desc string }{"n", "reject"},
		)
	}

	if queueVisible {
		keys = append(keys, struct{ key, desc string }{"Esc", "close queue"})
	} else {
		keys = append(keys, struct{ key, desc string }{"Q", "queue"})
	}

	if hasPending {
		keys = append(keys, struct{ key, desc string }{"A", "approve all safe"})
	}

	keys = append(keys, struct{ key, desc string }{"q", "quit"})

	line := ""
	for i, k := range keys {
		if i > 0 {
			line += "  "
		}
		line += styleHintKey.Render(k.key) + " " + k.desc
	}

	return styleHintsBar.Width(width).Render(line)
}
