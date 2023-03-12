package localhost

type Config struct {
	Enabled bool `yaml:"enabled" validate:"required,eq=true" default:"true"`
}
