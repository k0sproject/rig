// Package config provides tools for getting data from OpenSSH config files
package config

import (
	"reflect"
	"strconv"

	"github.com/kevinburke/ssh_config"
)

// DefaultOptions is set to the default values for host "*" from ssh_config on init
var defaultOptions *Options
var DefaultFieldSet *FieldSet
var KnownFields []string

// Options has fields for all the settings available from ssh config files
type Options struct {
	Host string

	BatchMode                        bool
	BindAddress                      string
	ChallengeResponseAuthentication  bool
	CheckHostIP                      bool
	Ciphers                          string
	ClearAllForwardings              bool
	Compression                      bool
	CompressionLevel                 int
	ConnectionAttempts               int
	ConnectTimeout                   int
	ControlMaster                    bool
	ControlPath                      string
	DynamicForward                   string
	EnableSSHKeysign                 bool
	EscapeChar                       string
	ExitOnForwardFailure             bool
	ForwardAgent                     bool
	ForwardX11                       bool
	ForwardX11Trusted                bool
	GatewayPorts                     bool
	GlobalKnownHostsFile             string
	GSSAPIAuthentication             bool
	GSSAPIDelegateCredentials        bool
	GSSAPIRenewalForcesRekey         bool
	GSSAPITrustDNS                   bool
	HashKnownHosts                   bool
	HostbasedAuthentication          bool
	HostKeyAlgorithms                string
	HostKeyAlias                     string
	HostName                         string
	IdentitiesOnly                   bool
	IdentityFile                     []string
	KbdInteractiveAuthentication     bool
	LocalCommand                     string
	LocalForward                     string
	LogLevel                         string
	MACs                             string
	NoHostAuthenticationForLocalhost bool
	NumberOfPasswordPrompts          int
	PasswordAuthentication           bool
	PermitLocalCommand               bool
	Port                             int
	PreferredAuthentications         string
	Protocol                         int
	ProxyCommand                     string
	PublicKeyAuthentication          bool
	RekeyLimit                       string
	RemoteForward                    string
	RhostsRSAAuthentication          bool
	RSAAuthentication                bool
	SendEnv                          []string
	ServerAliveCountMax              int
	ServerAliveInterval              int
	SmartcardDevice                  string
	StrictHostKeyChecking            bool
	TCPKeepAlive                     bool
	Tunnel                           bool
	TunnelDevice                     string
	UsePrivilegedPort                bool
	User                             string
	UserKnownHostsFile               string
	VerifyHostKeyDNS                 bool
	VisualHostKey                    bool
	XAuthLocation                    string

	fieldSet *FieldSet
	isSet    map[string]bool
}

type FieldSet struct {
	Fields         []string
	defaultOptions *Options
}

func (f *FieldSet) GetOptions(host string) *Options {
	opts := &Options{Host: host, fieldSet: f}
	opts.populate()
	return opts
}

func NewFieldSet(fields []string) *FieldSet {
	fs := &FieldSet{Fields: fields}
	fs.defaultOptions = fs.GetOptions("*")
	return fs
}

func getString(host, field string) string {
	return ssh_config.Get(host, field)
}

func getStringAll(host, field string) []string {
	return ssh_config.GetAll(host, field)
}

func getBool(host, field string) bool {
	return ssh_config.Get(host, field) == "yes"
}

func getInt(host, field string) int {
	val := ssh_config.Get(host, field)
	if val == "" {
		return 0
	}
	if i, err := strconv.Atoi(val); err == nil {
		return i
	}
	return 0
}

func (o *Options) getField(name string) reflect.Value {
	return reflect.Indirect(reflect.ValueOf(o)).FieldByName(name)
}

func (o *Options) populate() {
	for _, fieldName := range o.fieldSet.Fields {
		field := o.getField(fieldName)
		if !field.CanSet() {
			continue
		}

		if ssh_config.SupportsMultiple(fieldName) {
			field.Set(reflect.ValueOf(getStringAll(o.Host, fieldName)))
			if defaultOptions != nil {
				defaultField := defaultOptions.getField(fieldName)
				o.isSet[fieldName] = !reflect.DeepEqual(field.Interface(), defaultField.Interface())
			}
			continue
		}
		switch field.Kind() { //nolint:exhaustive
		case reflect.String:
			field.Set(reflect.ValueOf(getString(o.Host, fieldName)))
		case reflect.Bool:
			field.Set(reflect.ValueOf(getBool(o.Host, fieldName)))
		case reflect.Int:
			field.Set(reflect.ValueOf(getInt(o.Host, fieldName)))
		default:
			continue
		}
		if defaultOptions != nil {
			defaultField := defaultOptions.getField(fieldName)
			o.isSet[fieldName] = !reflect.DeepEqual(field.Interface(), defaultField.Interface())
		}
	}
}

// GetOptions returns an Options struct for the given host
func GetOptions(host string) *Options {
	return DefaultFieldSet.GetOptions(host)
}

func init() {
	opt := Options{}
	obj := reflect.ValueOf(opt)
	KnownFields = []string{}
	for i := 0; i < obj.NumField(); i++ {
		f := obj.Type().Field(i)
		if f.Name == "Host" {
			continue
		}
		KnownFields = append(KnownFields, f.Name)
	}
	DefaultFieldSet = NewFieldSet(KnownFields)
}
