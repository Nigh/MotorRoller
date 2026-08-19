package main

import (
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"MotorRoller/usb"
)

type AppService struct {
	app *application.App

	mu      sync.Mutex
	session *usb.Session
	connID  string
}

func (a *AppService) setApp(app *application.App) {
	a.app = app
}

// ListDevices returns TinyKnob VID/PID matches.
func (a *AppService) ListDevices() ([]usb.DeviceInfo, error) {
	return usb.ListDevices()
}

// Connect opens Vendor Bulk on the given device id ("bus:addr").
func (a *AppService) Connect(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		a.session.Close()
		a.session = nil
		a.connID = ""
	}
	sess, err := usb.Connect(id, func(snap usb.Snapshot) {
		if a.app != nil {
			a.app.Event.Emit("telemetry", snap)
		}
	})
	if err != nil {
		return err
	}
	a.session = sess
	a.connID = id
	return nil
}

// Disconnect closes the active session.
func (a *AppService) Disconnect() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		a.session.Close()
		a.session = nil
		a.connID = ""
	}
}

// ConnectedID returns the current device id or empty.
func (a *AppService) ConnectedID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connID
}

// SendCommand sends a TinyKnob opcode by name: START, STOP, SPRING, SPIN, TEST.
func (a *AppService) SendCommand(name string) error {
	var cmd byte
	switch name {
	case "START":
		cmd = usb.CmdStart
	case "STOP":
		cmd = usb.CmdStop
	case "SPRING":
		cmd = usb.CmdSpring
	case "SPIN":
		cmd = usb.CmdSpin
	case "TEST":
		cmd = usb.CmdTest
	default:
		return fmt.Errorf("unknown command %q", name)
	}
	a.mu.Lock()
	sess := a.session
	a.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("not connected")
	}
	return sess.SendCommand(cmd)
}

func (a *AppService) IsFrameless() bool {
	return isFrameless
}
