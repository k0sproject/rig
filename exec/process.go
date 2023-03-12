package exec

type Process interface {
	Wait() error
}
