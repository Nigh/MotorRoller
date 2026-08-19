package usb

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestParseTelem(t *testing.T) {
	buf := make([]byte, 16)
	buf[0] = TelemMagic
	buf[1] = 6 // MOTOR_SPRING
	binary.LittleEndian.PutUint32(buf[2:6], uint32(int32(3142))) // ~π rad in mrad
	binary.LittleEndian.PutUint16(buf[6:8], uint16(int16(16383)))
	binary.LittleEndian.PutUint16(buf[8:10], uint16(int16(0)))
	binary.LittleEndian.PutUint16(buf[10:12], uint16(int16(32767)))
	binary.LittleEndian.PutUint16(buf[12:14], 42)

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
}

func TestParseTelemRejects(t *testing.T) {
	if _, ok := ParseTelem([]byte{0x5A, 1, 1}); ok {
		t.Fatal("ack should not parse as telem")
	}
	if _, ok := ParseTelem([]byte{TelemMagic}); ok {
		t.Fatal("short frame")
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

func TestPackGoto(t *testing.T) {
	b := PackGoto(3142)
	if len(b) != 5 || b[0] != CmdGoto {
		t.Fatalf("%v", b)
	}
	if int32(binary.LittleEndian.Uint32(b[1:])) != 3142 {
		t.Fatalf("payload %v", b[1:])
	}
}

func TestModePos(t *testing.T) {
	if ModeName(9) != "MOTOR_POS" {
		t.Fatal(ModeName(9))
	}
}
