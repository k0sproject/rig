package ssh

import ssh "golang.org/x/crypto/ssh"

// PasswordCallback is a function that is called when a passphrase is needed to decrypt a private key
type PasswordCallback func() (secret string, err error)

// Config describes an SSH connection's configuration
type Config struct {
	Address          string           `yaml:"address" validate:"required,hostname_rfc1123|ip"`
	User             string           `yaml:"user" validate:"required" default:"root"`
	Port             int              `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath          *string          `yaml:"keyPath" validate:"omitempty"`
	HostKey          string           `yaml:"hostKey,omitempty"`
	Bastion          *Connection      `yaml:"bastion,omitempty"` // TODO: validate that bastion is not the same as the current client, also the unmarshaling needs to be fixed
	PasswordCallback PasswordCallback `yaml:"-"`

	// AuthMethods can be used to pass in a list of crypto/ssh.AuthMethod objects
	// for example to use a private key from memory:
	//   ssh.PublicKeys(privateKey)
	// For convenience, you can use ParseSSHPrivateKey() to parse a private key:
	//   authMethods, err := ssh.ParseSSHPrivateKey(key, rig.DefaultPassphraseCallback)
	AuthMethods []ssh.AuthMethod `yaml:"-"`
}

// Connection returns a new Connection object based on the configuration
func (c *Config) Connection() (*Connection, error) {
	return NewConnection(*c)
}
