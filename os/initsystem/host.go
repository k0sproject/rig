package initsystem

type Host interface {
	Execf(string, ...interface{}) error
	ExecOutputf(string, ...interface{}) (string, error)
}
