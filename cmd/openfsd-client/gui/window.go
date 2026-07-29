//go:build !nogui

package gui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/renorris/openfsd/internal/clientinject"
)

// controller owns form widgets and wires them to the engine.
//
// Concurrency: c.mu protects form, closed, busy. Widget mutations run only on
// the Fyne UI thread (callbacks or fyne.Do). Apply/Revert/Launch run on workers.
type controller struct {
	eng *clientinject.Engine
	app fyne.App
	win fyne.Window

	mu     sync.Mutex
	form   FormState
	closed bool
	busy   bool

	slots       []ClientSlot
	slotByLabel map[string]ClientSlot
	enabledOnly []ClientSlot

	clientSelect *widget.Select
	installEntry *widget.Entry
	detectBtn    *widget.Button

	webBaseEntry   *widget.Entry
	fsdHostEntry   *widget.Entry
	fsdPortEntry   *widget.Entry
	fsdServerEntry *widget.Entry
	afvBaseEntry   *widget.Entry
	preferShortJWT *widget.Check
	forceNoVoice   *widget.Check

	statusLbl *widget.Label
	applyBtn  *widget.Button
	revertBtn *widget.Button
	launchBtn *widget.Button
}

func newController(eng *clientinject.Engine, a fyne.App, w fyne.Window) *controller {
	c := &controller{
		eng:  eng,
		app:  a,
		win:  w,
		form: DefaultFormState(),
	}
	all := BuildClientSlots(eng.Adapters)
	c.slots = all
	c.slotByLabel = make(map[string]ClientSlot, len(all))
	for _, s := range all {
		c.slotByLabel[SlotLabel(s)] = s
		if s.Enabled {
			c.enabledOnly = append(c.enabledOnly, s)
		}
	}
	return c
}

func (c *controller) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

func (c *controller) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *controller) formCopy() FormState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.form
}

func (c *controller) updateForm(fn func(*FormState)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.form)
}

func (c *controller) buildUI() fyne.CanvasObject {
	// --- Install path first ---
	// Client Select.SetSelected runs OnChanged immediately and may call onDetect,
	// which writes installEntry. Creating the entry after SetSelected panics
	// (nil *widget.Entry.SetText) — observed on Windows GUI boot.
	c.installEntry = widget.NewEntry()
	c.installEntry.SetPlaceHolder("Client folder")
	c.installEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.InstallPath = s })
	}
	c.detectBtn = widget.NewButton("Detect", func() { c.onDetect(true) })
	browseBtn := widget.NewButton("Browse…", func() {
		c.onBrowse()
	})
	installRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(c.detectBtn, browseBtn),
		c.installEntry,
	)

	// --- Client (enabled only) ---
	var labels []string
	var firstEnabled string
	for _, s := range c.enabledOnly {
		lab := SlotLabel(s)
		labels = append(labels, lab)
		if firstEnabled == "" {
			firstEnabled = lab
		}
	}
	if len(labels) == 0 {
		labels = []string{"(no clients)"}
	}
	c.clientSelect = widget.NewSelect(labels, func(sel string) {
		slot, ok := c.slotByLabel[sel]
		if !ok || !slot.Enabled {
			return
		}
		c.updateForm(func(f *FormState) { f.ClientID = slot.ID })
		// If path empty, auto-detect for the new client.
		if strings.TrimSpace(c.formCopy().InstallPath) == "" {
			c.onDetect(false)
		}
	})
	if firstEnabled != "" {
		c.clientSelect.SetSelected(firstEnabled)
		if s, ok := c.slotByLabel[firstEnabled]; ok {
			c.updateForm(func(f *FormState) { f.ClientID = s.ID })
		}
	}

	// --- Endpoints ---
	c.webBaseEntry = widget.NewEntry()
	c.webBaseEntry.SetPlaceHolder("https://fsd.example.com")
	c.webBaseEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.WebBaseURL = s })
	}
	c.fsdHostEntry = widget.NewEntry()
	c.fsdHostEntry.SetPlaceHolder("fsd.example.com")
	c.fsdHostEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.FSDHost = s })
	}
	c.fsdPortEntry = widget.NewEntry()
	c.fsdPortEntry.SetText(FormatPortString(0))
	c.fsdPortEntry.OnChanged = func(s string) {
		n, err := ParsePortString(s)
		if err != nil {
			return
		}
		c.updateForm(func(f *FormState) { f.FSDPort = n })
	}
	c.fsdServerEntry = widget.NewEntry()
	c.fsdServerEntry.SetText("OPENFSD")
	c.fsdServerEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.FSDServerName = s })
	}
	c.afvBaseEntry = widget.NewEntry()
	c.afvBaseEntry.SetPlaceHolder("optional")
	c.afvBaseEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.AFVBaseURL = s })
	}
	c.preferShortJWT = widget.NewCheck("Prefer short JWT", func(v bool) {
		c.updateForm(func(f *FormState) { f.PreferShortJWTPath = v })
	})
	c.forceNoVoice = widget.NewCheck("Disable voice", func(v bool) {
		c.updateForm(func(f *FormState) { f.ForceDisableAFV = v })
	})

	// Port field is short by default; give it 50% more width so "6809" isn't cramped.
	portMin := c.fsdPortEntry.MinSize()
	portField := container.New(
		layout.NewGridWrapLayout(fyne.NewSize(portMin.Width*1.5, portMin.Height)),
		c.fsdPortEntry,
	)
	hostPort := container.NewBorder(nil, nil, nil,
		container.NewHBox(widget.NewLabel("Port"), portField),
		c.fsdHostEntry,
	)

	endpoints := widget.NewForm(
		widget.NewFormItem("Web URL", c.webBaseEntry),
		widget.NewFormItem("FSD host", hostPort),
		widget.NewFormItem("Server", c.fsdServerEntry),
		widget.NewFormItem("AFV URL", c.afvBaseEntry),
	)

	// --- Status + actions ---
	c.statusLbl = widget.NewLabel("")
	c.statusLbl.Wrapping = fyne.TextWrapWord

	c.applyBtn = widget.NewButton("Apply", func() { c.onApply() })
	c.applyBtn.Importance = widget.HighImportance
	c.revertBtn = widget.NewButton("Revert", func() { c.onRevert() })
	c.launchBtn = widget.NewButton("Launch", func() { c.onLaunch() })
	actions := container.NewHBox(c.applyBtn, c.revertBtn, c.launchBtn, layout.NewSpacer())

	// Compact single-column layout — no scroll, fits 640×480.
	return container.NewPadded(container.NewVBox(
		widget.NewForm(widget.NewFormItem("Client", c.clientSelect)),
		widget.NewForm(widget.NewFormItem("Install", installRow)),
		widget.NewSeparator(),
		endpoints,
		container.NewHBox(c.preferShortJWT, c.forceNoVoice),
		layout.NewSpacer(),
		c.statusLbl,
		actions,
	))
}

func (c *controller) setStatus(msg string) {
	if c.statusLbl != nil {
		c.statusLbl.SetText(msg)
	}
}

func (c *controller) syncClientSelectFromForm() {
	id := c.formCopy().ClientID
	for _, s := range c.enabledOnly {
		if s.ID == id {
			c.clientSelect.SetSelected(SlotLabel(s))
			return
		}
	}
}

func (c *controller) loadSettingsAndRefresh() {
	path, err := DefaultSettingsPath()
	if err == nil {
		if s, err := LoadSettings(path); err == nil {
			c.updateForm(func(f *FormState) { s.ApplyToForm(f) })
		}
	}
	f := c.formCopy()
	// Ensure client is still enabled.
	if f.ClientID != "" {
		found := false
		for _, s := range c.enabledOnly {
			if s.ID == f.ClientID {
				found = true
				break
			}
		}
		if !found && len(c.enabledOnly) > 0 {
			c.updateForm(func(form *FormState) { form.ClientID = c.enabledOnly[0].ID })
		}
	}
	c.syncClientSelectFromForm()
	f = c.formCopy()
	c.installEntry.SetText(f.InstallPath)
	c.webBaseEntry.SetText(f.WebBaseURL)
	c.fsdHostEntry.SetText(f.FSDHost)
	c.fsdPortEntry.SetText(FormatPortString(f.FSDPort))
	if f.FSDServerName != "" {
		c.fsdServerEntry.SetText(f.FSDServerName)
	}
	c.afvBaseEntry.SetText(f.AFVBaseURL)
	c.preferShortJWT.SetChecked(f.PreferShortJWTPath)
	c.forceNoVoice.SetChecked(f.ForceDisableAFV)

	// Auto-detect when no path saved.
	if strings.TrimSpace(f.InstallPath) == "" {
		c.onDetect(false)
	}
}

func (c *controller) saveSettings() {
	path, err := DefaultSettingsPath()
	if err != nil {
		return
	}
	_ = SaveSettings(path, SettingsFromForm(c.formCopy()))
}

func (c *controller) setBusy(v bool) {
	c.mu.Lock()
	c.busy = v
	c.mu.Unlock()
	if v {
		c.applyBtn.Disable()
		c.revertBtn.Disable()
		c.launchBtn.Disable()
		c.applyBtn.SetText("Working…")
		return
	}
	c.applyBtn.Enable()
	c.revertBtn.Enable()
	c.launchBtn.Enable()
	c.applyBtn.SetText("Apply")
}

func (c *controller) onDetect(showMiss bool) {
	if c.installEntry == nil {
		return
	}
	f := c.formCopy()
	cand, ok := FirstDetectPath(c.eng, f.ClientID)
	if !ok {
		if showMiss {
			c.setStatus("No install found — enter path")
		}
		return
	}
	c.installEntry.SetText(cand.RootDir)
	c.setStatus("Found " + cand.RootDir)
}

// onBrowse opens the OS-native folder picker off the UI thread.
func (c *controller) onBrowse() {
	go func() {
		path, ok := browseFolder()
		fyne.Do(func() {
			if c.isClosed() {
				return
			}
			if !ok {
				// Cancel or no picker available — stay quiet on cancel.
				return
			}
			c.installEntry.SetText(path)
		})
	}()
}

func (c *controller) onApply() {
	f := c.formCopy()
	issues := ValidateForm(f)
	if len(issues) > 0 {
		c.setStatus(issues[0].Message)
		return
	}
	if IsPublicVATSIMHost(f.WebBaseURL) {
		c.setStatus("Use a different web URL")
		return
	}

	c.setBusy(true)
	c.setStatus("Applying…")
	eng := c.eng
	go func() {
		install := BuildInstall(eng, f.ClientID, f.InstallPath)
		if pe := clientinject.AbsPrimaryPE(install); pe != "" {
			if err := clientinject.PreflightPrimaryPE(pe); err != nil {
				if errors.Is(err, clientinject.ErrClientRunning) {
					fyne.Do(func() {
						if c.isClosed() {
							return
						}
						c.setBusy(false)
						c.setStatus("Quit the client first")
					})
					return
				}
			}
		}
		plan, err := eng.Plan(context.Background(), install, f.Endpoints())
		if err != nil {
			fyne.Do(func() {
				if c.isClosed() {
					return
				}
				c.setBusy(false)
				c.setStatus(shortErr(err))
			})
			return
		}
		if len(plan.Blockers) > 0 {
			fyne.Do(func() {
				if c.isClosed() {
					return
				}
				c.setBusy(false)
				c.setStatus(plan.Blockers[0])
			})
			return
		}
		res, err := eng.Apply(context.Background(), plan)
		fyne.Do(func() {
			if c.isClosed() {
				return
			}
			c.setBusy(false)
			if err != nil {
				c.setStatus(shortErr(err))
				return
			}
			c.saveSettings()
			c.setStatus(fmt.Sprintf("Done — %d changes", len(res.Applied)))
		})
	}()
}

func (c *controller) onRevert() {
	f := c.formCopy()
	root := strings.TrimSpace(f.InstallPath)
	if root == "" {
		c.setStatus("set install path")
		return
	}
	c.setBusy(true)
	c.setStatus("Reverting…")
	eng := c.eng
	go func() {
		err := eng.Revert(context.Background(), filepath.Clean(root))
		fyne.Do(func() {
			if c.isClosed() {
				return
			}
			c.setBusy(false)
			if err != nil {
				c.setStatus(shortErr(err))
				return
			}
			c.setStatus("Reverted")
		})
	}()
}

func (c *controller) onLaunch() {
	f := c.formCopy()
	if strings.TrimSpace(f.InstallPath) == "" {
		c.setStatus("set install path")
		return
	}
	c.setBusy(true)
	c.setStatus("Launching…")
	eng := c.eng
	go func() {
		install := BuildInstall(eng, f.ClientID, f.InstallPath)
		a, ok := eng.Adapters[f.ClientID]
		if !ok {
			fyne.Do(func() {
				if c.isClosed() {
					return
				}
				c.setBusy(false)
				c.setStatus("unknown client")
			})
			return
		}
		args := a.LaunchArgs(install, f.Endpoints())
		pe := clientinject.AbsPrimaryPE(install)
		if pe == "" {
			fyne.Do(func() {
				if c.isClosed() {
					return
				}
				c.setBusy(false)
				c.setStatus("no client binary found")
			})
			return
		}
		cmd := exec.Command(pe, args...)
		cmd.Dir = install.RootDir
		startErr := cmd.Start()
		if startErr == nil {
			_ = cmd.Process.Release()
		}
		fyne.Do(func() {
			if c.isClosed() {
				return
			}
			c.setBusy(false)
			if startErr != nil {
				c.setStatus(shortErr(startErr))
				return
			}
			c.setStatus("Started")
		})
	}()
}

func shortErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 80 {
		return msg[:79] + "…"
	}
	return msg
}
