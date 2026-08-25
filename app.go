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

func (a *AppService) sessionOrErr() (*usb.Session, error) {
	a.mu.Lock()
	sess := a.session
	a.mu.Unlock()
	if sess == nil {
		return nil, fmt.Errorf("not connected")
	}
	return sess, nil
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
	}, func(event usb.ModeCogScaleEvent) {
		if a.app != nil {
			a.app.Event.Emit("cog-scale", event)
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

// SendCommand sends a TinyKnob opcode by name: START, STOP, SPRING, SPIN, TEST, STRESS, GEAR, UPLOAD.
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
	case "STRESS":
		cmd = usb.CmdStress
	case "GEAR":
		cmd = usb.CmdGear
	case "UPLOAD":
		cmd = usb.CmdUpload
	default:
		return fmt.Errorf("unknown command %q", name)
	}
	sess, err := a.sessionOrErr()
	if err != nil {
		return err
	}
	if err := sess.SendCommand(cmd); err != nil {
		return err
	}
	if name == "UPLOAD" {
		// Device reboots into UF2; drop the dead session.
		a.Disconnect()
	}
	return nil
}

// Goto streams MOTOR_POS setpoint (angle in milliradians, unwrapped).
func (a *AppService) Goto(angleMrad int32) error {
	sess, err := a.sessionOrErr()
	if err != nil {
		return err
	}
	pkt := usb.PackGoto(angleMrad)
	return sess.SendCommand(pkt[0], pkt[1:]...)
}

// SetK sets spring stiffness: K = kx10/10, kx10 clamped to 0…80.
func (a *AppService) SetK(kx10 uint8) error {
	if kx10 > 80 {
		kx10 = 80
	}
	sess, err := a.sessionOrErr()
	if err != nil {
		return err
	}
	return sess.SendCommand(usb.CmdSetK, kx10)
}

// SetRest sets spring rest angle to the current encoder position.
func (a *AppService) SetRest() error {
	sess, err := a.sessionOrErr()
	if err != nil {
		return err
	}
	return sess.SendCommand(usb.CmdSetRest)
}

// SetCogScale sets the runtime cogging feed-forward scale (0…2000 = 0…2).
func (a *AppService) SetCogScale(scaleX1000 uint16) error {
	sess, err := a.sessionOrErr()
	if err != nil {
		return err
	}
	pkt := usb.PackCogScale(scaleX1000)
	return sess.SendCommand(pkt[0], pkt[1:]...)
}

func (a *AppService) IsFrameless() bool {
	return isFrameless
}
