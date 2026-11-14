package macnative

import "testing"

func TestActivationRejectsOldNotifications(t *testing.T) {
	for _, tc := range []struct {
		source, target, current int32
		want                    bool
	}{
		{10, 20, 20, true}, {20, 20, 20, false}, {10, 20, 30, false}, {10, 99, 99, false}, {10, 0, 0, false},
	} {
		if got := activationChanged(tc.source, tc.target, tc.current, 99); got != tc.want {
			t.Fatalf("%+v: %t", tc, got)
		}
	}
}
