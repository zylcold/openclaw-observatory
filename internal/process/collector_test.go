package process

import "testing"

func TestParseCPUTime(t *testing.T) {
	for input, want := range map[string]float64{"01:02.50": 62.5, "02:03:04": 7384, "1-00:00:01": 86401} {
		got, err := parseCPUTime(input)
		if err != nil || got != want {
			t.Fatalf("%s: got %v err %v want %v", input, got, err, want)
		}
	}
}

func TestDiskSpace(t *testing.T) {
	total, available, err := diskSpace()
	if err != nil || total <= 0 || available < 0 || available > total {
		t.Fatalf("unexpected disk space total=%d available=%d err=%v", total, available, err)
	}
}
