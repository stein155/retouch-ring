package ring

// OLED notifications for the SoundTouch 20 front panel: a bell for a doorbell
// press, a motion icon for motion, with a short localized line under it. The
// panel contents are snapshotted before the first draw and restored a few
// seconds later, so whatever the firmware was showing comes back untouched.
// Other SoundTouch models expose no such framebuffer; OledAvailable() is false
// there and everything here is a no-op.

import (
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch-ring/internal/oled"
)

const fbPath = "/dev/fb0"

// showFor is how long a notification stays on the panel.
const showFor = 8 * time.Second

// LangFunc supplies the UI language for on-display texts. The plugin wires this
// to ReTouch's language setting; standalone runs default to English.
var LangFunc = func() string { return "en" }

var (
	displayMu    sync.Mutex
	fb           = oled.NewFramebuffer(fbPath)
	hasOLED      = oled.Available(fbPath)
	restoreTimer *time.Timer
)

// OledAvailable reports whether this speaker has the ST20 OLED panel.
func OledAvailable() bool { return hasOLED }

// ShowEvent draws the notification for kind ("ding" or "motion") on the OLED,
// with the device name worked into the text when known, and schedules the
// panel restore. Safe to call on any model; a no-op without the panel.
func ShowEvent(lang, kind, device string) {
	if !hasOLED {
		return
	}
	if fb.Draw(renderEvent(lang, kind, device)) != nil {
		return
	}
	displayMu.Lock()
	defer displayMu.Unlock()
	if restoreTimer != nil {
		restoreTimer.Stop()
	}
	restoreTimer = time.AfterFunc(showFor, func() { _ = fb.Restore() })
}

// renderEvent builds the notification frame: the event icon centered with the
// localized line(s) under it.
func renderEvent(lang, kind, device string) []byte {
	art, text := bellIcon, Tr(lang, "oled.ding")
	if device != "" {
		text = Tr(lang, "oled.ding.at", device)
	}
	if kind == "motion" {
		art = motionIcon
		text = Tr(lang, "oled.motion")
		if device != "" {
			text = Tr(lang, "oled.motion.at", device)
		}
	}
	c := oled.NewCanvas()
	lines := oled.Wrap(strings.ToUpper(text), 20, 3)
	textTop := 90 - (len(lines)-1)*oled.TextHeight
	w, h := oled.SpriteSize(art)
	iconTop := textTop - h - 4
	if iconTop < 2 {
		iconTop = 2
	}
	c.Sprite((oled.Width-w)/2, iconTop, art, 255, 150)
	for i, line := range lines {
		c.TextCentered(textTop+i*oled.TextHeight, line, 255)
	}
	c.Rect(0, 0, oled.Width-1, oled.Height-1, 70)
	return c.Pix()
}

var bellIcon = []string{
	"                      ####",
	"                      ####",
	"                      ####",
	"                    ########",
	"                    ########",
	"                   ##########",
	"                  ############",
	"                  ############",
	"                 ##############",
	"   ++            ##############                ++",
	"  +++           ################               +++",
	"  ++            ################                ++",
	"  +   +        ##################            +   +",
	"    +++        ##################            +++",
	"    +++       ####################           +++",
	"   +++        ####################            +++",
	"   ++        ######################            ++",
	"   ++        ######################            ++",
	"   +         ######################             +",
	"   ++       ########################           ++",
	"   ++       ########################           ++",
	"   +++      ########################          +++",
	"    +++     ########################         +++",
	"    +++    ##########################        +++",
	"  +   +    ##########################        +   +",
	"  ++       ##########################           ++",
	"  +++      ##########################          +++",
	"   ++      ##########################          ++",
	"           ##########################",
	"           ##########################",
	"           ##########################",
	"          ############################",
	"       ##################################",
	"       ##################################",
	"       ##################################",
	"       ##################################",
	"       ##################################",
	"",
	"                        #",
	"                      #####",
	"                     #######",
	"                     #######",
	"                    #########",
	"                     #######",
	"                     #######",
	"                      #####",
	"                        #",
}

var motionIcon = []string{
	"                        ###",
	"                        ####",
	"                          ###",
	"                           ###",
	"                            ##",
	"                            ###",
	"                   ##        ###",
	"                   ###        ##",
	"                    ###        ##",
	"                     ###       ##",
	"                      ###       ##",
	"                       ##       ##",
	"               #        ##       ##",
	"              ###       ###      ##",
	"               ###       ##      ##",
	"                ###      ##       ##",
	"                 ##      ###      ##",
	"       #         ###      ##      ##",
	"    #######       ##      ##      ##",
	"   #########      ##      ##      ##",
	"   #########      ##      ##      ##",
	"   #########       #       #       #",
	"  ###########      #       #       #",
	"   #########       #       #       #",
	"   #########      ##      ##      ##",
	"   #########      ##      ##      ##",
	"    #######       ##      ##      ##",
	"       #         ###      ##      ##",
	"                 ##      ###      ##",
	"                ###      ##       ##",
	"               ###       ##      ##",
	"              ###       ###      ##",
	"               #        ##       ##",
	"                       ##       ##",
	"                      ###       ##",
	"                     ###       ##",
	"                    ###        ##",
	"                   ###        ##",
	"                   ##        ###",
	"                            ###",
	"                            ##",
	"                           ###",
	"                          ###",
	"                        ####",
	"                        ###",
}
