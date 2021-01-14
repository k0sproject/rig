package initsystem

type host interface {
	Execf(string, ...interface{}) error
	ExecOutputf(string, ...interface{}) (string, error)
}
