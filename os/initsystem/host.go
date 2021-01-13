package initsystem

type Host interface {
	Execf(string, ...interface{}) error
	ExecWithOutputf(string, ...interface{}) (string, error)
}
