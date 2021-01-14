package initsystem

type host interface {
	Execf(string, ...interface{}) error
	ExecWithOutputf(string, ...interface{}) (string, error)
}
