package platform

import (
	"os/exec"
	"runtime"
)

// OpenBrowser asks the desktop to open url: "open" on darwin, "xdg-open"
// elsewhere. The opener is started, not waited for.
func OpenBrowser(url string) error {
	return openBrowserWith(runtime.GOOS, startDetached, url)
}

func openCommand(goos string) string {
	if goos == "darwin" {
		return "open"
	}
	return "xdg-open"
}

func openBrowserWith(goos string, start func(name string, args ...string) error, url string) error {
	return start(openCommand(goos), url)
}

// startDetached starts the program and reaps it in the background.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
