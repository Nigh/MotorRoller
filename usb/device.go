package usb

import (
	"fmt"

	"github.com/google/gousb"
)

// DeviceInfo identifies one TinyKnob-compatible device for UI selection.
type DeviceInfo struct {
	ID           string `json:"id"` // "bus:addr"
	Bus          int    `json:"bus"`
	Address      int    `json:"address"`
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`
}

// ListDevices enumerates USB devices matching TinyKnob VID/PID.
func ListDevices() ([]DeviceInfo, error) {
	ctx := gousb.NewContext()
	defer ctx.Close()

	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		return desc.Vendor == gousb.ID(VID) && desc.Product == gousb.ID(PID)
	})
	if err != nil {
		// OpenDevices may still return partial results with an error.
		for _, d := range devs {
			_ = d.Close()
		}
		if len(devs) == 0 {
			return nil, err
		}
	}

	out := make([]DeviceInfo, 0, len(devs))
	for _, d := range devs {
		info := DeviceInfo{
			ID:      fmt.Sprintf("%d:%d", d.Desc.Bus, d.Desc.Address),
			Bus:     d.Desc.Bus,
			Address: d.Desc.Address,
		}
		if m, e := d.Manufacturer(); e == nil {
			info.Manufacturer = m
		}
		if p, e := d.Product(); e == nil {
			info.Product = p
		}
		out = append(out, info)
		_ = d.Close()
	}
	return out, nil
}
