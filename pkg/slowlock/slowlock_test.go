package slowlock

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func hammerLocker(l sync.Locker, goroutines int, iterations int, maxDelay time.Duration) {
	wg := sync.WaitGroup{}
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				l.Lock()
				if maxDelay > 0 {
					time.Sleep(time.Duration(rand.Float32() * float32(maxDelay)))
				}
				l.Unlock()
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestHammerMutex(t *testing.T) {
	hammerLocker(&Mutex{}, 10, 1000, 0)
	rwm := &RWMutex{}
	hammerLocker(rwm, 10, 1000, 0)
	hammerLocker(rwm.RLocker(), 10, 1000, 0)
}

func TestSingleSlow(t *testing.T) {
	var logMsg *LogData
	logMsgLock := sync.Mutex{}
	logMessage := func(data LogData, lastSuccessful *LogData) {
		logMsgLock.Lock()
		defer logMsgLock.Unlock()
		logMsg = &data
	}
	testAnnotation := "test123"
	cfg := Config{
		Annotation:  testAnnotation,
		Timeout:     50 * time.Millisecond,
		LogFunction: logMessage,
	}
	for _, m := range []sync.Locker{cfg.Mutex(), cfg.RWMutex()} {
		m.Lock()
		go func() {
			time.Sleep(75 * time.Millisecond)
			m.Unlock()
		}()
		//nolint: all    // golangci-lint warns about empty critical section
		{
			m.Lock()
			m.Unlock()
		}
		logMsgLock.Lock()
		lm := logMsg
		logMsgLock.Unlock()
		assert.NotNil(t, lm)
		assert.Equal(t, lm.Annotation, testAnnotation)
	}
}

func TestHammerWithSlow(t *testing.T) {
	var logMsg *LogData
	var maxWait time.Duration
	logMsgLock := sync.Mutex{}
	logMessage := func(data LogData, lastSuccessful *LogData) {
		logMsgLock.Lock()
		defer logMsgLock.Unlock()
		logMsg = &data
		wait := time.Since(logMsg.StartTime)
		if wait > maxWait {
			maxWait = wait
		}
	}
	cfg := Config{
		Timeout:     10 * time.Millisecond,
		LogFunction: logMessage,
	}
	for _, m := range []sync.Locker{cfg.Mutex(), cfg.RWMutex()} {
		hammerLocker(m, 4, 50, 20*time.Millisecond)
		logMsgLock.Lock()
		lm := logMsg
		mw := maxWait
		logMsgLock.Unlock()
		assert.NotNil(t, lm)
		assert.Less(t, mw, 200*time.Millisecond)
	}
}
