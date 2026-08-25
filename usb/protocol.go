package usb

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	VID = 0xACDC
	PID = 0x4011

	EPOut = 0x01
	EPIn  = 0x81

	TelemMagic = 0xA5
	AckMagic   = 0x5A
	EventMagic = 0x5B

	EventModeCogScale = 0x01
	CogScaleEventSize = 5

	TelemSize = 25

	CmdStart       = 0x01
	CmdStop        = 0x02
	CmdSpring      = 0x03
	CmdSpin        = 0x04
	CmdTest        = 0x05
	CmdGoto        = 0x06
	CmdStress      = 0x07
	CmdGear        = 0x0A
	CmdSetK        = 0x20
	CmdSetRest     = 0x21
	CmdSetCogScale = 0x22
	CmdUpload      = 0x7F // reboot into UF2 bootloader; no ACK
)

// Telemetry is one decoded Bulk IN frame (magic 0xA5).
type Telemetry struct {
	Mode      uint8   `json:"mode"`
	ModeName  string  `json:"modeName"`
	AngleMrad int32   `json:"angleMrad"`
	DutyA     float64 `json:"dutyA"`
	DutyB     float64 `json:"dutyB"`
	DutyC     float64 `json:"dutyC"`
	Seq       uint16  `json:"seq"`
	IdA       float64 `json:"idA"`    // measured Id (amps)
	IqA       float64 `json:"iqA"`    // measured Iq (amps)
	IqRefA    float64 `json:"iqRefA"` // Iq reference (amps)
	VbusV     float64 `json:"vbusV"`  // bus voltage (volts)
	Uq        float64 `json:"uq"`     // current-loop Uq (Q15 → [-1,1])
	Dir       int8    `json:"dir"`    // motor/encoder direction (-1 or +1)
}

// ModeCogScaleEvent reports the active SPRING or SPIN cogging scale.
type ModeCogScaleEvent struct {
	Mode       uint8  `json:"mode"`
	ScaleX1000 uint16 `json:"scaleX1000"`
}

func ModeName(mode uint8) string {
	switch mode {
	case 0:
		return "MOTOR_IDLE"
	case 1:
		return "MOTOR_ALIGN_RAMP"
	case 2:
		return "MOTOR_ALIGN_HOLD"
	case 3:
		return "MOTOR_DIR_PULSE"
	case 4:
		return "MOTOR_ALIGN_DOWN"
	case 5:
		return "MOTOR_TEST"
	case 6:
		return "MOTOR_SPRING"
	case 7:
		return "MOTOR_SPIN"
	case 8:
		return "MOTOR_FAULT"
	case 9:
		return "MOTOR_POS"
	case 10:
		return "MOTOR_STRESS"
	case 11:
		return "MOTOR_COG_CAL"
	case 12:
		return "MOTOR_GEAR"
	default:
		return fmt.Sprintf("UNKNOWN_%d", mode)
	}
}

func q15ToDuty(v int16) float64 {
	return float64(v) / 32767.0
}

// ParseTelem unpacks a 25-byte telemetry frame. Returns false if not telemetry.
func ParseTelem(data []byte) (Telemetry, bool) {
	if len(data) < TelemSize || data[0] != TelemMagic {
		return Telemetry{}, false
	}
	mode := data[1]
	angle := int32(binary.LittleEndian.Uint32(data[2:6]))
	da := int16(binary.LittleEndian.Uint16(data[6:8]))
	db := int16(binary.LittleEndian.Uint16(data[8:10]))
	dc := int16(binary.LittleEndian.Uint16(data[10:12]))
	seq := binary.LittleEndian.Uint16(data[12:14])
	idMA := int16(binary.LittleEndian.Uint16(data[14:16]))
	iqMA := int16(binary.LittleEndian.Uint16(data[16:18]))
	iqRefMA := int16(binary.LittleEndian.Uint16(data[18:20]))
	vbusMV := binary.LittleEndian.Uint16(data[20:22])
	uqQ15 := int16(binary.LittleEndian.Uint16(data[22:24]))
	dir := int8(data[24])
	return Telemetry{
		Mode:      mode,
		ModeName:  ModeName(mode),
		AngleMrad: angle,
		DutyA:     q15ToDuty(da),
		DutyB:     q15ToDuty(db),
		DutyC:     q15ToDuty(dc),
		Seq:       seq,
		IdA:       float64(idMA) / 1000.0,
		IqA:       float64(iqMA) / 1000.0,
		IqRefA:    float64(iqRefMA) / 1000.0,
		VbusV:     float64(vbusMV) / 1000.0,
		Uq:        q15ToDuty(uqQ15),
		Dir:       dir,
	}, true
}

// IsAck reports a short ACK packet (magic 0x5A).
func IsAck(data []byte) bool {
	return len(data) >= 3 && data[0] == AckMagic
}

// ParseModeCogScaleEvent decodes a 0x5B mode COG-scale event.
func ParseModeCogScaleEvent(data []byte) (ModeCogScaleEvent, bool) {
	if len(data) < CogScaleEventSize || data[0] != EventMagic || data[1] != EventModeCogScale {
		return ModeCogScaleEvent{}, false
	}
	return ModeCogScaleEvent{
		Mode:       data[2],
		ScaleX1000: binary.LittleEndian.Uint16(data[3:5]),
	}, true
}

// AngleRad converts milliradians to radians.
func AngleRad(mrad int32) float64 {
	return float64(mrad) / 1000.0
}

// AngleDeg converts milliradians to degrees.
func AngleDeg(mrad int32) float64 {
	return AngleRad(mrad) * 180.0 / math.Pi
}

// WrapAngleRad wraps radians to [0, 2π).
func WrapAngleRad(rad float64) float64 {
	const twoPi = 2 * math.Pi
	r := math.Mod(rad, twoPi)
	if r < 0 {
		r += twoPi
	}
	return r
}

// PackGoto builds Bulk OUT: opcode 0x06 + little-endian int32 angle_mrad.
func PackGoto(angleMrad int32) []byte {
	b := make([]byte, 5)
	b[0] = CmdGoto
	binary.LittleEndian.PutUint32(b[1:], uint32(angleMrad))
	return b
}

// PackCogScale builds SET_COG_SCALE with scale encoded as scale*1000.
func PackCogScale(scaleX1000 uint16) []byte {
	if scaleX1000 > 2000 {
		scaleX1000 = 2000
	}
	b := make([]byte, 3)
	b[0] = CmdSetCogScale
	binary.LittleEndian.PutUint16(b[1:], scaleX1000)
	return b
}
