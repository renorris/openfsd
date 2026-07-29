//go:build !nogui

package gui

import (
	"os/exec"
	"runtime"
	"strings"
)

// browseFolder opens an OS-native folder picker (not Fyne's custom dialog).
// Returns ("", false) on cancel or if no picker is available.
// Safe to call from a worker goroutine; does not touch Fyne widgets.
func browseFolder() (string, bool) {
	switch runtime.GOOS {
	case "darwin":
		return browseFolderDarwin()
	case "windows":
		return browseFolderWindows()
	default:
		return browseFolderUnix()
	}
}

func browseFolderDarwin() (string, bool) {
	// NSOpenPanel via AppleScript — real macOS folder sheet/dialog.
	out, err := exec.Command("osascript", "-e",
		`try
			POSIX path of (choose folder with prompt "Select client install folder")
		on error
			return ""
		end try`).CombinedOutput()
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(string(out))
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return "", false
	}
	return path, true
}

func browseFolderWindows() (string, bool) {
	// Native Windows folder picker (WinForms FolderBrowserDialog via STA PowerShell).
	ps := `Add-Type -AssemblyName System.Windows.Forms; $fb = New-Object System.Windows.Forms.FolderBrowserDialog; $fb.Description = 'Select client install folder'; $fb.ShowNewFolderButton = $false; if ($fb.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($fb.SelectedPath) }`
	out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", ps).CombinedOutput()
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", false
	}
	return path, true
}

func browseFolderUnix() (string, bool) {
	// Prefer portal/zenity/kdialog — all host OS folder dialogs.
	if out, err := exec.Command("zenity", "--file-selection", "--directory",
		"--title=Select client install folder").CombinedOutput(); err == nil {
		path := strings.TrimSpace(string(out))
		if path != "" {
			return path, true
		}
	}
	if out, err := exec.Command("kdialog", "--getexistingdirectory", ".",
		"--title", "Select client install folder").CombinedOutput(); err == nil {
		path := strings.TrimSpace(string(out))
		if path != "" {
			return path, true
		}
	}
	// xdg-desktop-portal via dbus-send is awkward; yad as last resort.
	if out, err := exec.Command("yad", "--file", "--directory",
		"--title=Select client install folder").CombinedOutput(); err == nil {
		path := strings.TrimSpace(string(out))
		if path != "" {
			return path, true
		}
	}
	return "", false
}
