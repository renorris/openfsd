package afv

import "time"

func doSleep(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
