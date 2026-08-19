package usb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/gousb"
)

const (
	waveCapacity = 120 // ~2s at 60 Hz emit
	emitInterval = time.Second / 50
)

// Snapshot is a throttled telemetry push for the UI.
type Snapshot struct {
	Telemetry
	DutyAHist []float64 `json:"dutyAHist"`
	DutyBHist []float64 `json:"dutyBHist"`
	DutyCHist []float64 `json:"dutyCHist"`
}

// EmitFn is called with UI snapshots (~50 Hz).
type EmitFn func(Snapshot)

// Session owns one open Vendor Bulk connection.
type Session struct {
	mu     sync.Mutex
	ctx    *gousb.Context
	dev    *gousb.Device
	intf   *gousb.Interface
	done   []func()
	in     *gousb.InEndpoint
	out    *gousb.OutEndpoint
	cancel context.CancelFunc
	wg     sync.WaitGroup

	emit EmitFn

	// ring for waveform (written on read loop, copied on emit)
	histMu sync.Mutex
	aHist  []float64
	bHist  []float64
	cHist  []float64
	latest Telemetry
	have   bool
}

// Connect opens the device identified by "bus:addr" and starts the read loop.
func Connect(id string, emit EmitFn) (*Session, error) {
	var bus, addr int
	if _, err := fmt.Sscanf(id, "%d:%d", &bus, &addr); err != nil {
		return nil, fmt.Errorf("invalid device id %q: want bus:addr", id)
	}

	usbCtx := gousb.NewContext()
	devs, err := usbCtx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		return desc.Vendor == gousb.ID(VID) &&
			desc.Product == gousb.ID(PID) &&
			desc.Bus == bus &&
			desc.Address == addr
	})
	if err != nil && len(devs) == 0 {
		usbCtx.Close()
		return nil, err
	}
	if len(devs) == 0 {
		usbCtx.Close()
		return nil, fmt.Errorf("device %s not found", id)
	}
	for i := 1; i < len(devs); i++ {
		_ = devs[i].Close()
	}
	dev := devs[0]

	if err := dev.SetAutoDetach(true); err != nil {
		_ = dev.Close()
		usbCtx.Close()
		return nil, fmt.Errorf("SetAutoDetach: %w", err)
	}

	cfg, err := dev.ActiveConfigNum()
	if err != nil {
		_ = dev.Close()
		usbCtx.Close()
		return nil, err
	}
	config, err := dev.Config(cfg)
	if err != nil {
		_ = dev.Close()
		usbCtx.Close()
		return nil, err
	}

	var (
		claimed  *gousb.Interface
		done     []func()
		inEP     *gousb.InEndpoint
		outEP    *gousb.OutEndpoint
		lastErr  error
		sawClass bool
	)

	for _, iface := range config.Desc.Interfaces {
		for _, alt := range iface.AltSettings {
			if alt.Class != gousb.ClassVendorSpec {
				continue
			}
			sawClass = true
			// gousb: Interface(num, alternate) — Alternate is alt setting, not Number (iface id)
			intf, err := config.Interface(iface.Number, alt.Alternate)
			if err != nil {
				lastErr = err
				continue
			}
			var in *gousb.InEndpoint
			var out *gousb.OutEndpoint
			for _, ep := range alt.Endpoints {
				switch {
				case ep.Address == gousb.EndpointAddress(EPIn) && ep.Direction == gousb.EndpointDirectionIn:
					in, err = intf.InEndpoint(ep.Number)
					if err != nil {
						lastErr = err
						intf.Close()
						in, out = nil, nil
						break
					}
				case ep.Address == gousb.EndpointAddress(EPOut) && ep.Direction == gousb.EndpointDirectionOut:
					out, err = intf.OutEndpoint(ep.Number)
					if err != nil {
						lastErr = err
						intf.Close()
						in, out = nil, nil
						break
					}
				}
			}
			if in != nil && out != nil {
				claimed = intf
				inEP, outEP = in, out
				done = append(done, intf.Close, func() { _ = config.Close() })
				break
			}
			intf.Close()
			if lastErr == nil {
				lastErr = fmt.Errorf("vendor IF %d missing bulk 0x01/0x81", iface.Number)
			}
		}
		if claimed != nil {
			break
		}
	}

	if claimed == nil || inEP == nil || outEP == nil {
		_ = config.Close()
		_ = dev.Close()
		usbCtx.Close()
		if !sawClass {
			return nil, errors.New("vendor bulk interface (class 0xFF) not found")
		}
		if lastErr != nil {
			return nil, fmt.Errorf("claim vendor bulk interface: %w", lastErr)
		}
		return nil, errors.New("claim vendor bulk interface failed")
	}

	s := &Session{
		ctx:   usbCtx,
		dev:   dev,
		intf:  claimed,
		done:  done,
		in:    inEP,
		out:   outEP,
		emit:  emit,
		aHist: make([]float64, 0, waveCapacity),
		bHist: make([]float64, 0, waveCapacity),
		cHist: make([]float64, 0, waveCapacity),
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(2)
	go s.readLoop(runCtx)
	go s.emitLoop(runCtx)
	return s, nil
}

func (s *Session) readLoop(ctx context.Context) {
	defer s.wg.Done()
	buf := make([]byte, 64)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := s.in.Read(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(5 * time.Millisecond)
				continue
			}
		}
		data := buf[:n]
		if IsAck(data) {
			continue
		}
		t, ok := ParseTelem(data)
		if !ok {
			continue
		}
		s.histMu.Lock()
		s.latest = t
		s.have = true
		s.histMu.Unlock()
	}
}

func (s *Session) emitLoop(ctx context.Context) {
	defer s.wg.Done()
	tick := time.NewTicker(emitInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if s.emit == nil {
				continue
			}
			s.histMu.Lock()
			if !s.have {
				s.histMu.Unlock()
				continue
			}
			t := s.latest
			// ponytail: downsample to emit rate (~50 Hz); ceiling ~2s window — bump waveCapacity for longer
			s.aHist = appendRing(s.aHist, t.DutyA, waveCapacity)
			s.bHist = appendRing(s.bHist, t.DutyB, waveCapacity)
			s.cHist = appendRing(s.cHist, t.DutyC, waveCapacity)
			snap := Snapshot{
				Telemetry: t,
				DutyAHist: append([]float64(nil), s.aHist...),
				DutyBHist: append([]float64(nil), s.bHist...),
				DutyCHist: append([]float64(nil), s.cHist...),
			}
			s.histMu.Unlock()
			s.emit(snap)
		}
	}
}

func appendRing(hist []float64, v float64, capN int) []float64 {
	if len(hist) < capN {
		return append(hist, v)
	}
	copy(hist, hist[1:])
	hist[capN-1] = v
	return hist
}

// SendCommand writes a single-byte opcode (or opcode + payload) on Bulk OUT.
func (s *Session) SendCommand(cmd byte, payload ...byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.out == nil {
		return errors.New("not connected")
	}
	pkt := append([]byte{cmd}, payload...)
	_, err := s.out.Write(pkt)
	return err
}

// Close stops the read loop and releases USB resources.
func (s *Session) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.done) - 1; i >= 0; i-- {
		s.done[i]()
	}
	s.done = nil
	if s.dev != nil {
		_ = s.dev.Close()
		s.dev = nil
	}
	if s.ctx != nil {
		s.ctx.Close()
		s.ctx = nil
	}
	s.in, s.out, s.intf = nil, nil, nil
}
