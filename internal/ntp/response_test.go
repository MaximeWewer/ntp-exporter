package ntp

import (
	"errors"
	"testing"

	ntp "github.com/beevik/ntp"
)

func TestResponse_ReferenceIDString(t *testing.T) {
	tests := []struct {
		stratum uint8
		refid   uint32
		want    string
	}{
		{1, 0x47505300, "GPS"},
		{1, 0x50505300, "PPS"},
		{0, 0x52415445, "RATE"},
		{2, 0xC0000201, "192.0.2.1"},
		{3, 0x0A000001, "10.0.0.1"},
	}
	for _, tt := range tests {
		r := &Response{Stratum: tt.stratum, ReferenceID: tt.refid}
		if got := r.ReferenceIDString(); got != tt.want {
			t.Errorf("stratum %d refid %08x: got %q, want %q", tt.stratum, tt.refid, got, tt.want)
		}
	}
}

func TestResponse_InvalidReason(t *testing.T) {
	tests := []struct {
		resp *Response
		want string
	}{
		{&Response{Stratum: 2}, ""},
		{&Response{Stratum: 0, KissCode: "DENY"}, ReasonKoD},
		{&Response{Stratum: 2, LeapIndicator: 3}, ReasonUnsync},
		{&Response{Stratum: 16}, ReasonStratum},
		{&Response{Stratum: 2, ValidateError: ntp.ErrInvalidDispersion}, ReasonRootDistance},
		{&Response{Stratum: 2, ValidateError: ntp.ErrServerClockFreshness}, ReasonStaleReference},
		{&Response{Stratum: 2, ValidateError: ntp.ErrServerTickedBackwards}, ReasonInvalidTime},
		{&Response{Stratum: 2, ValidateError: errors.New("boom")}, ReasonOther},
	}
	for _, tt := range tests {
		if got := tt.resp.InvalidReason(); got != tt.want {
			t.Errorf("%+v: got %q, want %q", tt.resp, got, tt.want)
		}
	}
}
