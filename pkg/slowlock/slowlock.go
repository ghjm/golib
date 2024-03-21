package slowlock

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
)

type CallerInfo struct {
	File string
	Line int
}

type LogData struct {
	Annotation string
	StartTime  time.Time
	CallerInfo *CallerInfo
}

// String returns a string representation of the log data.
func (ld *LogData) String() string {
	var descr string
	if ld.Annotation != "" {
		descr += fmt.Sprintf(" %s", ld.Annotation)
	}
	if ld.CallerInfo != nil {
		descr += fmt.Sprintf(" at %s line %d", ld.CallerInfo.File, ld.CallerInfo.Line)
	}
	msg := fmt.Sprintf("#### SLOW LOCK ####: %s since %s (%s)",
		descr,
		ld.StartTime,
		time.Now().Sub(ld.StartTime))
	return msg
}

// LogFunction is the type of callback that will be called when a slow lock is detected
type LogFunction func(data LogData, lastSuccessful *LogData)

// printLog prints to the default logger
func printLog(data LogData, lastSuccessful *LogData) {
	msg := data.String()
	if lastSuccessful != nil {
		msg = msg + ". Last successful: " + lastSuccessful.String()
	}
	log.Print(msg)
}

var (
	defaultTimeout     time.Duration = 10 * time.Second
	defaultLogFunction LogFunction   = printLog
)

// SetDefaultTimeout sets the default timeout before a lock is considered to be slow.
func SetDefaultTimeout(t time.Duration) {
	defaultTimeout = t
}

// SetDefaultLogFunction sets the default logging callback for slow locks.
func SetDefaultLogFunction(lf LogFunction) {
	defaultLogFunction = lf
}

// Config is an optional type that allows individual configuration of locks.
type Config struct {
	Annotation  string
	Timeout     time.Duration
	LogFunction LogFunction
}

// Mutex returns a new Mutex with the given configuration.
func (c Config) Mutex() *Mutex {
	return &Mutex{
		lockTracker: lockTracker{
			Config: c,
		},
		mut: sync.Mutex{},
	}
}

// RWMutex returns a new RWMutex with the given configuration.
func (c Config) RWMutex() *RWMutex {
	return &RWMutex{
		lockTracker: lockTracker{
			Config: c,
		},
		mut: sync.RWMutex{},
	}
}

type lockTracker struct {
	Config
	lastSuccessfulLock *LogData
	lockLock           sync.RWMutex
}

func (lt *lockTracker) acquireLock(lockFunc func(), logData LogData) {
	acquiredCh := make(chan struct{})
	timeout := lt.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ticker := time.NewTicker(timeout)
	go func() {
		for {
			select {
			case <-ticker.C:
				lt.lockLock.RLock()
				lf := lt.LogFunction
				lsl := lt.lastSuccessfulLock
				lt.lockLock.RUnlock()
				if lf == nil {
					lf = defaultLogFunction
				}
				lf(logData, lsl)
			case <-acquiredCh:
				ticker.Stop()
				return
			}
		}
	}()
	lockFunc()
	close(acquiredCh)
	lt.lockLock.Lock()
	lt.lastSuccessfulLock = &logData
	lt.lockLock.Unlock()
}

// Mutex is a drop-in replacement for sync.Mutex that provides logging of slow lock acquisitions.
type Mutex struct {
	lockTracker
	mut sync.Mutex
}

// Lock locks m.  If the lock is not acquired before the timeout, logs will be generated.
func (m *Mutex) Lock() {
	ld := LogData{
		Annotation: m.Annotation,
		StartTime:  time.Now(),
	}
	_, file, line, ok := runtime.Caller(1)
	if ok {
		ld.CallerInfo = &CallerInfo{
			File: file,
			Line: line,
		}
	}
	m.acquireLock(func() { m.mut.Lock() }, ld)
}

// TryLock attempts to lock m.  Slow locks will not be tracked.
func (m *Mutex) TryLock() bool {
	return m.mut.TryLock()
}

// Unlock unlocks m.
func (m *Mutex) Unlock() {
	m.mut.Unlock()
}

// RWMutex is a drop-in replacement for sync.RWMutex that provides logging of slow lock acquisitions.
type RWMutex struct {
	lockTracker
	trackReadLocks bool
	mut            sync.RWMutex
}

// SetTrackReadLocks sets whether slow lock tracking should be enabled for read locks.
// If false, then read locks are passed through to the underlying sync.RWMutex with no tracking.
func (rw *RWMutex) SetTrackReadLocks(track bool) {
	rw.trackReadLocks = track
}

// Lock locks rw for read/write.  If the lock is not acquired before the timeout, logs will be generated.
func (rw *RWMutex) Lock() {
	ld := LogData{
		Annotation: rw.Annotation,
		StartTime:  time.Now(),
	}
	_, file, line, ok := runtime.Caller(0)
	if ok {
		ld.CallerInfo = &CallerInfo{
			File: file,
			Line: line,
		}
	}
	rw.acquireLock(func() { rw.mut.Lock() }, ld)
}

// TryLock attempts to lock rw for read/write.  Slow locks will not be tracked.
func (rw *RWMutex) TryLock() bool {
	return rw.mut.TryLock()
}

// Unlock releases the read/write lock on rw.
func (rw *RWMutex) Unlock() {
	rw.mut.Unlock()
}

// RLock locks rw for read.  If the lock is not acquired before the timeout, logs will be generated.
func (rw *RWMutex) RLock() {
	if !rw.trackReadLocks {
		rw.mut.RLock()
		return
	}
	ld := LogData{
		Annotation: rw.Annotation,
		StartTime:  time.Now(),
	}
	_, file, line, ok := runtime.Caller(0)
	if ok {
		ld.CallerInfo = &CallerInfo{
			File: file,
			Line: line,
		}
	}
	rw.acquireLock(func() { rw.mut.RLock() }, ld)
}

// TryRLock attempts to lock rw for read.  Slow locks will not be tracked.
func (rw *RWMutex) TryRLock() bool {
	return rw.mut.TryRLock()
}

// RUnlock releases the read lock on rw.
func (rw *RWMutex) RUnlock() {
	rw.mut.RUnlock()
}

// RLocker returns a Locker interface that implements
// the Lock and Unlock methods by calling rw.RLock and rw.RUnlock.
func (rw *RWMutex) RLocker() sync.Locker {
	return (*rlocker)(rw)
}

type rlocker RWMutex

func (r *rlocker) Lock()   { (*RWMutex)(r).RLock() }
func (r *rlocker) Unlock() { (*RWMutex)(r).RUnlock() }
