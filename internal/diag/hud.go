// Package diag holds the command-line checks: reading the HUD from
// screenshots, and testing the virtual DualSense's rumble, input and
// haptics.
package diag

import (
	"fmt"
	"strings"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/elite"
	"github.com/tolgahan/ed-sense/internal/hud"
	"github.com/tolgahan/ed-sense/internal/hud/vision"
)

// HUD reads screenshots (Elite's F10 screenshots are BMP files in
// Pictures\Frontier Developments\Elite Dangerous) with the colours EDSense
// would use, then looks for the colours on screen the way EDSense does when
// it can't read the shield %.
func HUD(files []string, cfg *config.Config, dataDir string) {
	if len(files) == 0 {
		fmt.Println("usage: EDSense.exe -hudtest <screenshot.bmp|png|jpg> ...")
		return
	}
	pal, source, _ := hud.ResolvePalette(cfg.HUDColors, dataDir)
	fmt.Printf("HUD colours: %s (shield %s, heat %s)\n", source, pal.Shield.Hex(), pal.Heat.Hex())
	mods := loadoutModules(cfg)
	if len(mods) > 0 {
		fmt.Printf("Ship modules for the fire group lists (latest Loadout): %d\n", len(mods))
	}
	for _, f := range files {
		im, err := vision.LoadImage(f)
		if err != nil {
			fmt.Printf("%s: %v\n", f, err)
			continue
		}
		start := time.Now()
		r := hud.ReadAll(im, pal)
		fmt.Printf("%s [%dx%d, %d ms]\n", f, im.W, im.H, time.Since(start).Milliseconds())
		printReading(r, mods)

		start = time.Now()
		c := hud.CalibrateImage(im, pal)
		if !c.OK {
			fmt.Printf("  colour search: no shield %% / hull %% pair found (%d ms)\n", time.Since(start).Milliseconds())
			continue
		}
		fmt.Printf("  colour search (%d ms): shield %s (reads %s), HUD %s (hull %s), heat %s\n", time.Since(start).Milliseconds(),
			c.Shield.Hex(), c.ShieldText, c.Main.Hex(), c.HullText, c.Heat.Hex())
		found := pal.WithCalibration(c)
		r = hud.ReadAll(im, found)
		fmt.Println("  with those colours:")
		printReading(r, mods)
		if vision.ColorDistance(c.Shield, pal.Shield) >= 0.2 && r.Shield.Found() {
			fmt.Printf("  EDSense learns these on its own; to set them by hand: \"hud_colors\": {\"shield\": \"%s\", \"heat\": \"%s\"}\n", c.Shield.Hex(), c.Heat.Hex())
		}
	}
}

func loadoutModules(cfg *config.Config) []elite.Module {
	dir := cfg.JournalDir
	if dir == "" {
		dir = elite.JournalDir()
	}
	ev, ok := elite.LastEvent(dir, "Loadout")
	if !ok {
		return nil
	}
	return elite.LoadoutModules(ev)
}

func printReading(r hud.Reading, mods []elite.Module) {
	shield := "not found"
	if r.Shield.Found() {
		x, y := r.Shield.Center()
		shield = fmt.Sprintf("%s (match %.2f, at %.0f,%.0f, hit flash %.2f)", r.Shield.Text, r.Shield.Score, x, y, r.Splash)
	}
	fmt.Printf("  shield: %s\n", shield)
	fmt.Printf("  heat:   %s\n", number(r.Heat))
	fmt.Printf("  hull:   %s\n", number(r.Hull))
	caps := "not found"
	if r.CapsOK {
		caps = fmt.Sprintf("SYS %.0f%%, ENG %.0f%%, WEP %.0f%%", r.Caps[hud.SYS]*100, r.Caps[hud.ENG]*100, r.Caps[hud.WEP]*100)
	}
	fmt.Printf("  capacitors: %s\n", caps)
	switch {
	case !r.BlueZoneSeen:
		fmt.Println("  throttle: blue zone not found")
	case r.InBlueZone:
		fmt.Println("  throttle: in the blue zone")
	default:
		fmt.Println("  throttle: outside the blue zone")
	}
	target := "no target panel"
	if t := r.Target; t.Found() {
		target = fmt.Sprintf("shield %s, hull %s, hit flash %.2f", orNone(t.Shield.Text), orNone(t.Hull.Text), t.Splash)
	}
	fmt.Printf("  target: %s\n", target)
	fmt.Printf("  secondary (L2) list: %s\n", listText(r.Lists[hud.Secondary], mods))
	fmt.Printf("  primary (R2) list: %s\n", listText(r.Lists[hud.Primary], mods))
}

func number(n hud.Number) string {
	if !n.Found() {
		return "not found"
	}
	return fmt.Sprintf("%s (match %.2f)", n.Text, n.Score)
}

func listText(l hud.ListRead, mods []elite.Module) string {
	if len(l.Entries) == 0 {
		return "not found"
	}
	var parts []string
	for _, e := range l.Entries {
		name := fmt.Sprintf("? (%.1f wide)", e.Width)
		if i, _ := hud.MatchEntry(e, mods); i >= 0 {
			name = mods[i].Name
		}
		switch {
		case e.Red:
			name += " (out of range)"
		case e.Sub == hud.StatusLine && e.Clip < 0.15:
			name += " (reloading)"
		case e.Sub == hud.StatusLine:
			name += " (deploying)"
		case e.Sub == hud.AmmoLine:
			name += fmt.Sprintf(" (clip %.0f%%)", e.Clip*100)
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, ", ")
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
