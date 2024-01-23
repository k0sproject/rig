package initsystem

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/k0sproject/rig/exec"
)

// InitSystem defines the methods for interacting with an init system like OpenRC.
type InitSystem interface {
	StartService(ctx context.Context, h exec.ContextRunner, s string) error
	StopService(ctx context.Context, h exec.ContextRunner, s string) error
	ServiceScriptPath(ctx context.Context, h exec.ContextRunner, s string) (string, error)
	EnableService(ctx context.Context, h exec.ContextRunner, s string) error
	DisableService(ctx context.Context, h exec.ContextRunner, s string) error
	ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool
}

type InitSystemRestarter interface {
	RestartService(ctx context.Context, h exec.ContextRunner, s string) error
}

type InitSystemReloader interface {
	DaemonReload(ctx context.Context, h exec.ContextRunner) error
}

type InitSystemServiceEnvironment interface {
	ServiceEnvironmentPath(ctx context.Context, h exec.ContextRunner, s string) (string, error)
	ServiceEnvironmentContent(env map[string]string) string
}

type InitSystemFactory func(c exec.ContextRunner) InitSystem

var (
	DefaultInitSystemRepository = NewInitSystemRepository()
	repository                  atomic.Value
)

func init() {
	RegisterSystemd(DefaultInitSystemRepository)
	RegisterOpenRC(DefaultInitSystemRepository)
	RegisterUpstart(DefaultInitSystemRepository)
	RegisterSysVinit(DefaultInitSystemRepository)
	RegisterWinSCM(DefaultInitSystemRepository)
	RegisterRunit(DefaultInitSystemRepository)
	RegisterLaunchd(DefaultInitSystemRepository)

	SetRepository(DefaultInitSystemRepository)
}

func GetRepository() *InitSystemRepository {
	return repository.Load().(*InitSystemRepository)
}

func SetRepository(r *InitSystemRepository) {
	repository.Store(r)
}

type InitSystemRepository struct {
	systems map[string]InitSystemFactory
}

func (r *InitSystemRepository) Register(name string, factory InitSystemFactory) {
	r.systems[name] = factory
}

func (r *InitSystemRepository) Get(c exec.ContextRunner) (InitSystem, error) {
	for _, factory := range r.systems {
		system := factory(c)
		if system != nil {
			return system, nil
		}
	}
	return nil, fmt.Errorf("no init system found")
}

func NewInitSystemRepository() *InitSystemRepository {
	return &InitSystemRepository{
		systems: make(map[string]InitSystemFactory),
	}
}
