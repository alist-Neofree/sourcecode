package utils

import (
	"sync/atomic"
	"time"
)

var CNLoc = time.FixedZone("UTC", 8*60*60)

func MustParseCNTime(str string) time.Time {
	lastOpTime, _ := time.ParseInLocation("2006-01-02 15:04:05 -07", str+" +08", CNLoc)
	return lastOpTime
}

func NewDebounce(interval time.Duration) func(f func()) {
	var timer atomic.Value
	return func(f func()) {
		timer_ := timer.Load().(*time.Timer)
		if timer_ != nil {
			timer_.Stop()
		}
		timer_ = time.AfterFunc(interval, f)
		timer.Store(timer_)
	}
}

func NewDebounce2(interval time.Duration, f func()) func() {
	var timer atomic.Value
	return func() {
		timer_ := timer.Load().(*time.Timer)
		if timer_ == nil {
			timer_ = time.AfterFunc(interval, f)
			timer.Store(timer_)
		}
		timer_.Reset(interval)
	}
}

func NewThrottle(interval time.Duration) func(func()) {
	var lastCall atomic.Value
	lastCall.Store(time.Now())
	return func(fn func()) {
		now := time.Now()
		if now.Sub(lastCall.Load().(time.Time)) < interval {
			return
		}
		lastCall.Store(now)
		time.AfterFunc(interval, fn)
	}
}

func NewThrottle2(interval time.Duration, fn func()) func() {
	var lastCall atomic.Value
	lastCall.Store(time.Now())
	return func() {
		now := time.Now()
		if now.Sub(lastCall.Load().(time.Time)) < interval {
			return
		}
		lastCall.Store(now)
		time.AfterFunc(interval, fn)
	}
}
