package osrelease

import (
	"errors"
	"sync/atomic"
)

var (
	defaultResolver atomic.Value

	// ErrOSReleaseNotFound is returned when the OS release can't be found.
	ErrOSReleaseNotFound = errors.New("can't detect OS release")

	errNoMatch = errors.New("no match")
)

type runner interface {
	Run(string) (string, error)
	IsWindows() bool
}

type getter interface {
	Get(runner) (*OSRelease, error)
}

func DefaultResolver() getter {
	return defaultResolver.Load().(getter)
}

func SetDefaultResolver(r getter) {
	defaultResolver.Store(r)
}

type defaultRepositoryImpl struct {
	getters []getter
}

func (d *defaultRepositoryImpl) Get(r runner) (*OSRelease, error) {
	for _, getter := range d.getters {
		osRelease, err := getter.Get(r)
		if err == nil {
			return osRelease, nil
		}
	}
	return nil, ErrOSReleaseNotFound
}

func (d *defaultRepositoryImpl) Add(g getter) {
	d.getters = append(d.getters, g)
}

func init() {
	repo := &defaultRepositoryImpl{}
	repo.Add(&LinuxResolver{})
	repo.Add(&WindowsResolver{})
	repo.Add(&DarwinResolver{})
}
