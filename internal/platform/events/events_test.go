package events

import "testing"

func TestAttendanceTopicIsStable(t *testing.T) {
	if AttendanceTopic != "nexus.attendance.events" {
		t.Fatalf("unexpected attendance topic: %q", AttendanceTopic)
	}
}
