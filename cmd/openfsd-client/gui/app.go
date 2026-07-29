//go:build !nogui

package gui

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/adapters"
)

// Fixed 4:3 window — everything fits without scroll.
const (
	windowWidth  float32 = 640
	windowHeight float32 = 480
)

// Run launches the openfsd Client Setup GUI. Blocks until the window closes.
// engine may be nil — then DefaultEngine is used.
func Run(engine *clientinject.Engine) error {
	if engine == nil {
		var err error
		engine, err = adapters.DefaultEngine()
		if err != nil {
			return fmt.Errorf("gui: default engine: %w", err)
		}
	}

	a := app.NewWithID("com.openfsd.client-setup")

	w := a.NewWindow("openfsd Client Setup")
	w.Resize(fyne.NewSize(windowWidth, windowHeight))
	w.SetFixedSize(true)
	w.SetMaster()

	ctrl := newController(engine, a, w)
	w.SetContent(ctrl.buildUI())
	w.SetOnClosed(func() {
		ctrl.stop()
	})
	ctrl.loadSettingsAndRefresh()

	w.ShowAndRun()
	ctrl.stop()
	return nil
}

// RunDefault is the process entry for no-CLI-args mode.
func RunDefault() error {
	if os.Getenv("OPENFSD_CLIENT_NO_GUI") == "1" {
		return fmt.Errorf("gui: disabled by OPENFSD_CLIENT_NO_GUI=1")
	}
	if err := checkDisplayAvailable(); err != nil {
		return err
	}
	return Run(nil)
}

func checkDisplayAvailable() error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		if goosIsUnixDisplayRequired() {
			return fmt.Errorf("gui: no DISPLAY/WAYLAND_DISPLAY (headless?)")
		}
	}
	return nil
}

// CanStart reports whether a GUI attempt is reasonable (not a hard guarantee).
func CanStart() bool {
	if os.Getenv("OPENFSD_CLIENT_NO_GUI") == "1" {
		return false
	}
	return checkDisplayAvailable() == nil
}

// Ensure desktop driver is linked on platforms that use it.
var _ desktop.App
