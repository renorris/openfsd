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
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/renorris/openfsd/internal/clientinject"
)

// controller owns form widgets and wires them to the engine.
// All layout is client-agnostic; per-client behavior flows from Adapter + Plan.
//
// Concurrency: c.mu protects form, closed, busy, planTimer, lastPlan, and
// preflight. Widget mutations run only on the Fyne UI thread (callbacks or
// fyne.Do). Heavy Plan/Apply/hash work runs on worker goroutines.
type controller struct {
	eng *clientinject.Engine
	app fyne.App
	win fyne.Window

	mu        sync.Mutex
	form      FormState
	closed    bool
	busy      bool // Apply/Revert/Launch in flight
	planTimer *time.Timer
	lastPlan  *clientinject.Plan
	preflight error

	slots       []ClientSlot
	slotByLabel map[string]ClientSlot

	clientSelect *widget.Select
	installEntry *widget.Entry
	detectBtn    *widget.Button

	fingerprint *widget.Label
	configList  *widget.Label

	webBaseEntry    *widget.Entry
	fsdHostEntry    *widget.Entry
	fsdPortEntry    *widget.Entry
	fsdServerEntry  *widget.Entry
	afvBaseEntry    *widget.Entry
	forceNoVoice    *widget.Check
	preferShortJWT  *widget.Check
	publicVATSIMAck *widget.Check
	publicVATSIMLbl *widget.Label

	constraints *widget.Label
	blockers    *widget.Label
	warnings    *widget.Label
	mutations   *widget.Label

	applyBtn  *widget.Button
	revertBtn *widget.Button
	launchBtn *widget.Button

	logEntry *widget.Entry
}

func newController(eng *clientinject.Engine, a fyne.App, w fyne.Window) *controller {
	c := &controller{
		eng:  eng,
		app:  a,
		win:  w,
		form: DefaultFormState(),
	}
	c.slots = BuildClientSlots(eng.Adapters)
	c.slotByLabel = make(map[string]ClientSlot, len(c.slots))
	for _, s := range c.slots {
		c.slotByLabel[SlotLabel(s)] = s
	}
	return c
}

// stop cancels debounced dry-plan work and marks the controller closed so late
// fyne.Do callbacks skip widget updates after window teardown.
func (c *controller) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.planTimer != nil {
		c.planTimer.Stop()
		c.planTimer = nil
	}
}

func (c *controller) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// formCopy returns a snapshot of FormState under the form mutex.
func (c *controller) formCopy() FormState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.form
}

// updateForm mutates FormState under the form mutex (UI-thread writers).
func (c *controller) updateForm(fn func(*FormState)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.form)
}

func (c *controller) buildUI() fyne.CanvasObject {
	// --- Header ---
	title := widget.NewLabelWithStyle("openfsd Client Setup", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	legal := widget.NewLabel(LegalOneLiner)
	legal.Wrapping = fyne.TextWrapWord
	header := container.NewVBox(title, legal, widget.NewSeparator())

	// --- Client picker ---
	var labels []string
	var firstEnabled string
	for _, s := range c.slots {
		lab := SlotLabel(s)
		labels = append(labels, lab)
		if s.Enabled && firstEnabled == "" {
			firstEnabled = lab
		}
	}
	c.clientSelect = widget.NewSelect(labels, func(sel string) {
		slot, ok := c.slotByLabel[sel]
		if !ok || !slot.Enabled {
			c.syncClientSelectFromForm()
			c.appendLog("Client %q is not available yet.", sel)
			return
		}
		c.updateForm(func(f *FormState) { f.ClientID = slot.ID })
		c.schedulePlan()
	})
	if firstEnabled != "" {
		c.clientSelect.SetSelected(firstEnabled)
		if s, ok := c.slotByLabel[firstEnabled]; ok {
			c.updateForm(func(f *FormState) { f.ClientID = s.ID })
		}
	}

	// --- Install path ---
	c.installEntry = widget.NewEntry()
	c.installEntry.SetPlaceHolder("Client install directory")
	c.installEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.InstallPath = s })
		c.schedulePlan()
	}
	c.detectBtn = widget.NewButton("Detect", func() {
		c.onDetect()
	})
	browseBtn := widget.NewButton("Browse…", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			c.installEntry.SetText(uri.Path())
		}, c.win)
	})
	installRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(c.detectBtn, browseBtn),
		c.installEntry,
	)

	// --- Fingerprint + config ---
	c.fingerprint = widget.NewLabel("Fingerprint: (set install path)")
	c.fingerprint.Wrapping = fyne.TextWrapWord
	c.configList = widget.NewLabel("Config files: —")
	c.configList.Wrapping = fyne.TextWrapWord

	// --- Endpoints ---
	c.webBaseEntry = widget.NewEntry()
	c.webBaseEntry.SetPlaceHolder("https://fsd.example.com")
	c.webBaseEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.WebBaseURL = s })
		c.updatePublicVATSIMHint()
		c.schedulePlan()
	}
	c.fsdHostEntry = widget.NewEntry()
	c.fsdHostEntry.SetPlaceHolder("fsd.example.com")
	c.fsdHostEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.FSDHost = s })
		c.schedulePlan()
	}
	c.fsdPortEntry = widget.NewEntry()
	c.fsdPortEntry.SetText(FormatPortString(0))
	c.fsdPortEntry.OnChanged = func(s string) {
		n, err := ParsePortString(s)
		if err != nil {
			return
		}
		c.updateForm(func(f *FormState) { f.FSDPort = n })
		c.schedulePlan()
	}
	c.fsdServerEntry = widget.NewEntry()
	c.fsdServerEntry.SetText("OPENFSD")
	c.fsdServerEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.FSDServerName = s })
		c.schedulePlan()
	}
	c.afvBaseEntry = widget.NewEntry()
	c.afvBaseEntry.SetPlaceHolder("https://voice.example.com (optional)")
	c.afvBaseEntry.OnChanged = func(s string) {
		c.updateForm(func(f *FormState) { f.AFVBaseURL = s })
		c.schedulePlan()
	}
	c.forceNoVoice = widget.NewCheck("Force disable voice (-novoice)", func(v bool) {
		c.updateForm(func(f *FormState) { f.ForceDisableAFV = v })
		c.schedulePlan()
	})
	c.preferShortJWT = widget.NewCheck("Prefer short JWT path (/j, …)", func(v bool) {
		c.updateForm(func(f *FormState) { f.PreferShortJWTPath = v })
		c.schedulePlan()
	})
	c.publicVATSIMLbl = widget.NewLabel("")
	c.publicVATSIMLbl.Wrapping = fyne.TextWrapWord
	c.publicVATSIMAck = widget.NewCheck("I understand (public VATSIM host override)", func(v bool) {
		c.updateForm(func(f *FormState) { f.UnderstandPublicVATSIM = v })
		c.refreshApplyEnabled()
	})

	endpointsForm := widget.NewForm(
		widget.NewFormItem("Web base URL", c.webBaseEntry),
		widget.NewFormItem("FSD host", c.fsdHostEntry),
		widget.NewFormItem("FSD port", c.fsdPortEntry),
		widget.NewFormItem("Server name", c.fsdServerEntry),
		widget.NewFormItem("AFV base URL", c.afvBaseEntry),
	)

	// --- Constraints / plan panels ---
	c.constraints = widget.NewLabel(FormatConstraints(nil))
	c.constraints.Wrapping = fyne.TextWrapWord
	c.blockers = widget.NewLabel("")
	c.blockers.Wrapping = fyne.TextWrapWord
	c.warnings = widget.NewLabel("")
	c.warnings.Wrapping = fyne.TextWrapWord
	c.mutations = widget.NewLabel("")
	c.mutations.Wrapping = fyne.TextWrapWord

	// --- Actions ---
	c.applyBtn = widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
		c.onApply()
	})
	c.applyBtn.Importance = widget.HighImportance
	c.revertBtn = widget.NewButtonWithIcon("Revert", theme.ContentUndoIcon(), func() {
		c.onRevert()
	})
	c.launchBtn = widget.NewButtonWithIcon("Launch", theme.MediaPlayIcon(), func() {
		c.onLaunch()
	})
	actions := container.NewHBox(c.applyBtn, c.revertBtn, c.launchBtn)

	// --- Log ---
	c.logEntry = widget.NewMultiLineEntry()
	c.logEntry.SetMinRowsVisible(8)
	c.logEntry.Wrapping = fyne.TextWrapWord
	c.logEntry.Disable()

	body := container.NewVBox(
		header,
		widget.NewCard("Client", "", c.clientSelect),
		widget.NewCard("Install location", "", installRow),
		widget.NewCard("Fingerprint / preflight", "", container.NewVBox(c.fingerprint, c.configList)),
		widget.NewCard("openfsd endpoints", "", container.NewVBox(
			endpointsForm,
			c.forceNoVoice,
			c.preferShortJWT,
			c.publicVATSIMLbl,
			c.publicVATSIMAck,
		)),
		widget.NewCard("Plan constraints", "", c.constraints),
		widget.NewCard("Blockers", "", c.blockers),
		widget.NewCard("Warnings", "", c.warnings),
		widget.NewCard("Mutations", "", c.mutations),
		widget.NewCard("Actions", "", actions),
		widget.NewCard("Log", "", c.logEntry),
	)

	return container.NewVScroll(body)
}

func (c *controller) syncClientSelectFromForm() {
	id := c.formCopy().ClientID
	for _, s := range c.slots {
		if s.Enabled && s.ID == id {
			c.clientSelect.SetSelected(SlotLabel(s))
			return
		}
	}
}

func (c *controller) loadSettingsAndRefresh() {
	path, err := DefaultSettingsPath()
	if err != nil {
		c.appendLog("config dir: %v", err)
		return
	}
	s, err := LoadSettings(path)
	if err != nil {
		c.appendLog("load settings: %v", err)
		return
	}
	c.updateForm(func(f *FormState) {
		s.ApplyToForm(f)
		// Never restore public-VATSIM override across sessions.
		f.UnderstandPublicVATSIM = false
	})
	f := c.formCopy()
	if f.ClientID != "" {
		c.syncClientSelectFromForm()
	}
	c.installEntry.SetText(f.InstallPath)
	c.webBaseEntry.SetText(f.WebBaseURL)
	c.fsdHostEntry.SetText(f.FSDHost)
	c.fsdPortEntry.SetText(FormatPortString(f.FSDPort))
	if f.FSDServerName != "" {
		c.fsdServerEntry.SetText(f.FSDServerName)
	}
	c.afvBaseEntry.SetText(f.AFVBaseURL)
	c.forceNoVoice.SetChecked(f.ForceDisableAFV)
	c.preferShortJWT.SetChecked(f.PreferShortJWTPath)
	c.publicVATSIMAck.SetChecked(false)
	c.updatePublicVATSIMHint()
	c.schedulePlan()
	c.appendLog("Loaded settings from %s", path)
}

func (c *controller) saveSettings() {
	path, err := DefaultSettingsPath()
	if err != nil {
		return
	}
	if err := SaveSettings(path, SettingsFromForm(c.formCopy())); err != nil {
		c.appendLog("save settings: %v", err)
		return
	}
}

func (c *controller) appendLog(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	ts := time.Now().Format("15:04:05")
	cur := c.logEntry.Text
	if cur != "" && !strings.HasSuffix(cur, "\n") {
		cur += "\n"
	}
	c.logEntry.SetText(cur + ts + "  " + line + "\n")
	c.logEntry.CursorRow = len(strings.Split(c.logEntry.Text, "\n"))
}

func (c *controller) updatePublicVATSIMHint() {
	warn := PublicVATSIMWarning(c.formCopy().WebBaseURL)
	if warn == "" {
		c.publicVATSIMLbl.SetText("")
		return
	}
	c.publicVATSIMLbl.SetText("⚠ " + warn)
}

func (c *controller) schedulePlan() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if c.planTimer != nil {
		c.planTimer.Stop()
	}
	// Debounce dry-plan on keystroke / form change (runs off UI thread).
	c.planTimer = time.AfterFunc(350*time.Millisecond, func() {
		c.runDryPlan()
	})
}

// runDryPlan executes Plan / preflight / hash on a worker goroutine, then
// marshals UI updates via fyne.Do. Form is snapshotted under c.mu.
func (c *controller) runDryPlan() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	f := c.form
	c.mu.Unlock()

	issues := ValidateForm(f)

	install := BuildInstall(c.eng, f.ClientID, f.InstallPath)
	var preflightErr error
	if pe := clientinject.AbsPrimaryPE(install); pe != "" {
		if err := clientinject.PreflightPrimaryPE(pe); err != nil {
			preflightErr = err
		}
	}
	if install.HashSHA1 == "" && install.PrimaryPE != "" {
		if sum, err := fileSHA1OS(install.PrimaryPE); err == nil {
			install.HashSHA1 = sum
		}
	}
	profileID := install.ProfileID
	if profileID == "" && c.eng.Profiles != nil {
		if p, err := c.eng.ResolveProfile(install); err == nil {
			profileID = p.ProfileID
			install.ProfileID = p.ProfileID
		}
	}

	var plan *clientinject.Plan
	var planErr error
	if len(issues) == 0 && f.InstallPath != "" {
		plan, planErr = c.eng.Plan(context.Background(), install, f.Endpoints())
	}

	fyne.Do(func() {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		c.preflight = preflightErr
		c.lastPlan = plan
		c.mu.Unlock()

		c.fingerprint.SetText(FormatFingerprint(install, profileID, preflightErr))
		if len(install.ConfigPaths) > 0 {
			c.configList.SetText("Config files:\n  • " + strings.Join(install.ConfigPaths, "\n  • "))
		} else {
			c.configList.SetText("Config files: (none discovered yet)")
		}
		if plan != nil {
			c.constraints.SetText(FormatConstraints(plan.Constraints))
			if b := FormatBlockers(plan.Blockers); b != "" {
				c.blockers.SetText(b)
			} else {
				c.blockers.SetText("(none)")
			}
			if w := FormatWarnings(plan.Warnings); w != "" {
				c.warnings.SetText(w)
			} else {
				c.warnings.SetText("(none)")
			}
			c.mutations.SetText(FormatMutations(plan.Mutations))
		} else {
			msg := "(dry-plan not available)"
			if len(issues) > 0 {
				msg = "Form: " + issues[0].Message
			}
			if planErr != nil {
				msg = planErr.Error()
			}
			c.constraints.SetText(msg)
			c.blockers.SetText("")
			c.warnings.SetText("")
			c.mutations.SetText("")
		}
		c.refreshApplyEnabled()
	})
}

func (c *controller) refreshApplyEnabled() {
	c.mu.Lock()
	f := c.form
	plan := c.lastPlan
	preflight := c.preflight
	busy := c.busy
	c.mu.Unlock()

	if busy {
		c.applyBtn.Disable()
		c.revertBtn.Disable()
		c.launchBtn.Disable()
		c.applyBtn.SetText("Apply (working…)")
		return
	}

	issues := ValidateForm(f)
	ok, reason := CanApply(f, plan, issues)
	if preflight != nil && errors.Is(preflight, clientinject.ErrClientRunning) {
		ok = false
		reason = "client appears to be running — quit before Apply"
	}
	c.applyBtn.Enable()
	if !ok {
		c.applyBtn.Disable()
		if reason != "" {
			c.applyBtn.SetText("Apply (" + truncate(reason, 40) + ")")
		} else {
			c.applyBtn.SetText("Apply")
		}
	} else {
		c.applyBtn.SetText("Apply")
	}
	if strings.TrimSpace(f.InstallPath) == "" {
		c.revertBtn.Disable()
		c.launchBtn.Disable()
	} else {
		c.revertBtn.Enable()
		c.launchBtn.Enable()
	}
}

func (c *controller) setBusy(v bool) {
	c.mu.Lock()
	c.busy = v
	c.mu.Unlock()
	c.refreshApplyEnabled()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func (c *controller) onDetect() {
	f := c.formCopy()
	cand, ok := FirstDetectPath(c.eng, f.ClientID)
	if !ok {
		c.appendLog("No install detected for %s — set path manually.", f.ClientID)
		dialog.ShowInformation("Detect", "No install found for this client on this machine. Enter the install path manually.", c.win)
		return
	}
	c.installEntry.SetText(cand.RootDir)
	c.appendLog("Detected: %s (%s)", cand.RootDir, cand.DisplayHint)
}

func (c *controller) onApply() {
	f := c.formCopy()
	msg := LegalApplyBanner
	if warn := PublicVATSIMWarning(f.WebBaseURL); warn != "" {
		msg += "\n\n" + warn
	}
	c.mu.Lock()
	plan := c.lastPlan
	c.mu.Unlock()
	if plan != nil && len(plan.Blockers) > 0 {
		dialog.ShowError(fmt.Errorf("plan has blockers — fix endpoints first"), c.win)
		return
	}
	dialog.ShowConfirm("Apply — legal notice", msg+"\n\nApply patch to this install?", func(yes bool) {
		if !yes {
			c.appendLog("Apply cancelled")
			return
		}
		c.doApply()
	}, c.win)
}

func (c *controller) doApply() {
	f := c.formCopy()
	issues := ValidateForm(f)
	if len(issues) > 0 {
		dialog.ShowError(fmt.Errorf("%s", issues[0].Message), c.win)
		return
	}
	if warn := PublicVATSIMWarning(f.WebBaseURL); warn != "" && !f.UnderstandPublicVATSIM {
		dialog.ShowError(fmt.Errorf("public VATSIM host — check “I understand” or change Web base URL"), c.win)
		return
	}

	c.setBusy(true)
	c.appendLog("Planning / applying…")
	eng := c.eng
	go func() {
		install := BuildInstall(eng, f.ClientID, f.InstallPath)
		plan, err := eng.Plan(context.Background(), install, f.Endpoints())
		if err != nil {
			fyne.Do(func() {
				if c.isClosed() {
					return
				}
				c.setBusy(false)
				c.appendLog("plan: %v", err)
				dialog.ShowError(err, c.win)
			})
			return
		}
		if len(plan.Blockers) > 0 {
			fyne.Do(func() {
				if c.isClosed() {
					return
				}
				c.setBusy(false)
				c.appendLog("plan blocked: %s", strings.Join(plan.Blockers, "; "))
				dialog.ShowError(fmt.Errorf("plan blockers: %s", strings.Join(plan.Blockers, "; ")), c.win)
			})
			return
		}
		nMut := len(plan.Mutations)
		res, err := eng.Apply(context.Background(), plan)
		fyne.Do(func() {
			if c.isClosed() {
				return
			}
			c.setBusy(false)
			if err != nil {
				c.appendLog("apply failed: %v", err)
				dialog.ShowError(err, c.win)
				c.schedulePlan()
				return
			}
			c.saveSettings()
			c.appendLog("Applied OK manifest=%s applied=%v (%d mutations)", res.ManifestPath, res.Applied, nMut)
			dialog.ShowInformation("Apply complete", fmt.Sprintf("Patched successfully.\nManifest: %s\nApplied: %v", res.ManifestPath, res.Applied), c.win)
			c.schedulePlan()
		})
	}()
}

func (c *controller) onRevert() {
	f := c.formCopy()
	root := strings.TrimSpace(f.InstallPath)
	if root == "" {
		dialog.ShowError(fmt.Errorf("install path required"), c.win)
		return
	}
	dialog.ShowConfirm("Revert", "Restore stock files from .openfsd-bak for this install?", func(yes bool) {
		if !yes {
			return
		}
		c.setBusy(true)
		c.appendLog("Reverting…")
		eng := c.eng
		rootClean := filepath.Clean(root)
		go func() {
			err := eng.Revert(context.Background(), rootClean)
			fyne.Do(func() {
				if c.isClosed() {
					return
				}
				c.setBusy(false)
				if err != nil {
					c.appendLog("revert: %v", err)
					dialog.ShowError(err, c.win)
					return
				}
				c.appendLog("Reverted %s", rootClean)
				dialog.ShowInformation("Revert complete", "Stock files restored from backup.", c.win)
				c.schedulePlan()
			})
		}()
	}, c.win)
}

func (c *controller) onLaunch() {
	f := c.formCopy()
	c.setBusy(true)
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
				dialog.ShowError(fmt.Errorf("no adapter for %q", f.ClientID), c.win)
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
				dialog.ShowError(fmt.Errorf("no primary PE path"), c.win)
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
			c.appendLog("Launch: %s %s", pe, strings.Join(args, " "))
			if startErr != nil {
				c.appendLog("launch failed: %v (use printed args manually)", startErr)
				dialog.ShowInformation("Launch", fmt.Sprintf("Could not start process:\n%v\n\nCommand:\n%s %s\n\nWorking directory:\n%s",
					startErr, pe, strings.Join(args, " "), install.RootDir), c.win)
				return
			}
			c.appendLog("Started pid (detached)")
		})
	}()
}

func fileSHA1OS(path string) (string, error) {
	w := clientinject.OSFileWriter{}
	data, err := w.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha1SumHex(data), nil
}
