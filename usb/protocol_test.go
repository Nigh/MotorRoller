package usb

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestParseTelem(t *testing.T) {
	buf := make([]byte, 25)
	buf[0] = TelemMagic
	buf[1] = 6                                                   // MOTOR_SPRING
	binary.LittleEndian.PutUint32(buf[2:6], uint32(int32(3142))) // ~π rad in mrad
	binary.LittleEndian.PutUint16(buf[6:8], uint16(int16(16383)))
	binary.LittleEndian.PutUint16(buf[8:10], uint16(int16(0)))
	binary.LittleEndian.PutUint16(buf[10:12], uint16(int16(32767)))
	binary.LittleEndian.PutUint16(buf[12:14], 42)
	putI16 := func(off int, v int16) { binary.LittleEndian.PutUint16(buf[off:off+2], uint16(v)) }
	putI16(14, 123)                                  // Id 0.123 A
	putI16(16, -450)                                 // Iq -0.450 A
	putI16(18, 500)                                  // Iq_ref 0.500 A
	binary.LittleEndian.PutUint16(buf[20:22], 12000) // Vbus 12.000 V
	putI16(22, 16384)                                // Uq ≈ 0.5
	buf[24] = 0xff

	got, ok := ParseTelem(buf)
	if !ok {
		t.Fatal("expected ok")
	}
	if got.Mode != 6 || got.ModeName != "MOTOR_SPRING" {
		t.Fatalf("mode: %+v", got)
	}
	if got.AngleMrad != 3142 {
		t.Fatalf("angle %d", got.AngleMrad)
	}
	if math.Abs(got.DutyA-16383.0/32767.0) > 1e-9 {
		t.Fatalf("dutyA %v", got.DutyA)
	}
	if got.DutyB != 0 || math.Abs(got.DutyC-1.0) > 1e-9 {
		t.Fatalf("duty B/C %v %v", got.DutyB, got.DutyC)
	}
	if got.Seq != 42 {
		t.Fatalf("seq %d", got.Seq)
	}
	if math.Abs(got.IdA-0.123) > 1e-9 || math.Abs(got.IqA+0.45) > 1e-9 {
		t.Fatalf("Id/Iq %v %v", got.IdA, got.IqA)
	}
	if math.Abs(got.IqRefA-0.5) > 1e-9 || math.Abs(got.VbusV-12.0) > 1e-9 {
		t.Fatalf("IqRef/Vbus %v %v", got.IqRefA, got.VbusV)
	}
	if math.Abs(got.Uq-16384.0/32767.0) > 1e-9 {
		t.Fatalf("uq %v", got.Uq)
	}
	if got.Dir != -1 {
		t.Fatalf("dir %d", got.Dir)
	}
}

func TestParseTelemRejects(t *testing.T) {
	if _, ok := ParseTelem([]byte{0x5A, 1, 1}); ok {
		t.Fatal("ack should not parse as telem")
	}
	if _, ok := ParseTelem([]byte{TelemMagic}); ok {
		t.Fatal("short frame")
	}
	old24 := make([]byte, 24)
	old24[0] = TelemMagic
	if _, ok := ParseTelem(old24); ok {
		t.Fatal("24-byte frame must be rejected")
	}
}

func TestIsAck(t *testing.T) {
	if !IsAck([]byte{AckMagic, CmdStart, 1}) {
		t.Fatal("expected ack")
	}
	if IsAck([]byte{TelemMagic, 0}) {
		t.Fatal("telem is not ack")
	}
}

func TestParseModeCogScaleEvent(t *testing.T) {
	buf := []byte{EventMagic, EventModeCogScale, 7, 0, 0}
	binary.LittleEndian.PutUint16(buf[3:], 1250)
	got, ok := ParseModeCogScaleEvent(buf)
	if !ok || got.Mode != 7 || got.ScaleX1000 != 1250 {
		t.Fatalf("event: %+v, ok=%v", got, ok)
	}
	if _, ok := ParseModeCogScaleEvent([]byte{EventMagic, EventModeCogScale, 6}); ok {
		t.Fatal("short event must be rejected")
	}
	if _, ok := ParseModeCogScaleEvent([]byte{EventMagic, 2, 6, 0, 0}); ok {
		t.Fatal("unknown event must be rejected")
	}
	if _, ok := ParseModeCogScaleEvent([]byte{AckMagic, EventModeCogScale, 6, 0, 0}); ok {
		t.Fatal("ack must not parse as event")
	}
}

func TestPackGoto(t *testing.T) {
	b := PackGoto(3142)
	if len(b) != 5 || b[0] != CmdGoto {
		t.Fatalf("%v", b)
	}
	if int32(binary.LittleEndian.Uint32(b[1:])) != 3142 {
		t.Fatalf("payload %v", b[1:])
	}
}

func TestPackCogScale(t *testing.T) {
	b := PackCogScale(1250)
	if len(b) != 3 || b[0] != CmdSetCogScale || binary.LittleEndian.Uint16(b[1:]) != 1250 {
		t.Fatalf("payload %v", b)
	}
	b = PackCogScale(2500)
	if binary.LittleEndian.Uint16(b[1:]) != 2000 {
		t.Fatalf("clamped payload %v", b)
	}
}

func TestModePos(t *testing.T) {
	if ModeName(9) != "MOTOR_POS" {
		t.Fatal(ModeName(9))
	}
	if ModeName(10) != "MOTOR_STRESS" {
		t.Fatal(ModeName(10))
	}
	if ModeName(11) != "MOTOR_COG_CAL" {
		t.Fatal(ModeName(11))
	}
}

func TestCmdUpload(t *testing.T) {
	if CmdUpload != 0x7F {
		t.Fatalf("UPLOAD opcode %02x", CmdUpload)
	}
	if CmdStress != 0x07 {
		t.Fatalf("STRESS opcode %02x", CmdStress)
	}
}
