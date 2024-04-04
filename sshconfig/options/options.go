// Package options defines a number of types that represent options that can be set in the SSH configuration file.
package options

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	boolTrue   = "true"
	boolFalse  = "false"
	boolYes    = "yes"
	boolNo     = "no"
	optAsk     = "ask"
	optConfirm = "confirm"
	optAlways  = "always"
	optAuto    = "auto"
	optAutoAsk = "autoask"
	optNone    = "none"
)

var (
	errInvalidValue = errors.New("invalid value")

	userhome = sync.OnceValue(
		func() string {
			if home, err := os.UserHomeDir(); err == nil {
				return home
			}
			return ""
		},
	)
)

// BooleanOption is an option that can be set to yes or no.
type BooleanOption string

const (
	BooleanOptionNo  = BooleanOption(boolNo)
	BooleanOptionYes = BooleanOption(boolYes)
)

// IsTrue returns true if the option is set to yes.
func (b BooleanOption) IsTrue() bool {
	return b == boolYes
}

// IsFalse returns true if the option is set to no.
func (b BooleanOption) IsFalse() bool {
	return b == boolNo
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (b BooleanOption) Normalize() (BooleanOption, error) {
	switch string(b) {
	case boolNo, boolFalse:
		return BooleanOptionNo, nil
	case boolYes, boolTrue:
		return BooleanOptionYes, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for a boolean option", errInvalidValue, b)
}

// String returns the string representation of the option.
func (b BooleanOption) String() string {
	return string(b)
}

// CanonicalizeHostnameOption is an option that can be set to no, yes or always.
type CanonicalizeHostnameOption string

const (
	CanonicalizeHostnameNo     = CanonicalizeHostnameOption(boolNo)
	CanonicalizeHostnameYes    = CanonicalizeHostnameOption(boolYes)
	CanonicalizeHostnameAlways = CanonicalizeHostnameOption(optAlways)
)

var CanonicalizeHostnameOptions = map[string]CanonicalizeHostnameOption{
	boolNo:    CanonicalizeHostnameNo,
	boolFalse: CanonicalizeHostnameNo,
	boolYes:   CanonicalizeHostnameYes,
	boolTrue:  CanonicalizeHostnameYes,
	optAlways: CanonicalizeHostnameAlways,
}

// IsTrue returns true if the option is set to yes or always.
func (c CanonicalizeHostnameOption) IsTrue() bool {
	return c == boolYes || c == optAlways
}

// IsFalse returns true if the option is set to no.
func (c CanonicalizeHostnameOption) IsFalse() bool {
	return c == boolNo
}

// IsAlways returns true if the option is set to always.
func (c CanonicalizeHostnameOption) IsAlways() bool {
	return c == optAlways
}

// String returns the string representation of the option.
func (c CanonicalizeHostnameOption) String() string {
	return string(c)
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (c CanonicalizeHostnameOption) Normalize() (CanonicalizeHostnameOption, error) {
	if nv, ok := CanonicalizeHostnameOptions[string(c)]; ok {
		return nv, nil
	}

	return "", fmt.Errorf("%w: invalid value %q for CanonicalizeHostname", errInvalidValue, c)
}

// AddKeysToAgentOption is an extended boolean option that can be set to yes, no, ask, confirm,
// confirm with a time interval or a time interval.
type AddKeysToAgentOption string

var errNoInterval = errors.New("no interval")

const (
	AddKeysToAgentNo      = AddKeysToAgentOption(boolNo)
	AddKeysToAgentYes     = AddKeysToAgentOption(boolYes)
	AddKeysToAgentAsk     = AddKeysToAgentOption(optAsk)
	AddKeysToAgentConfirm = AddKeysToAgentOption(optConfirm)
)

// IsTrue returns true if the option is set to yes.
func (a AddKeysToAgentOption) IsTrue() bool {
	return a == boolYes
}

// IsFalse returns true if the option is set to no.
func (a AddKeysToAgentOption) IsFalse() bool {
	return a == boolNo
}

// HasInterval returns true if the option has an interval set.
func (a AddKeysToAgentOption) HasInterval() bool {
	return (a.IsConfirm() && a[len(a)-1] >= '0' && a[len(a)-1] <= '0') || (a[0] >= '0' && a[0] <= '9')
}

// IsAsk returns true if the option is set to ask.
func (a AddKeysToAgentOption) IsAsk() bool {
	return a == optAsk
}

// IsConfirm returns true if the option is set to confirm.
func (a AddKeysToAgentOption) IsConfirm() bool {
	return strings.HasPrefix(string(a), "confirm ")
}

// Interval returns the time interval set in the option. If the option does not have an interval an error is returned.
func (a AddKeysToAgentOption) Interval() (time.Duration, error) {
	if !a.HasInterval() {
		return 0, errNoInterval
	}
	str := strings.TrimPrefix(string(a), "confirm ")

	d, err := time.ParseDuration(AddIntervalSuffix(str))
	if err != nil {
		return 0, fmt.Errorf("invalid interval: %w", err)
	}
	return d, nil
}

// String returns the string representation of the option.
func (a AddKeysToAgentOption) String() string {
	return string(a)
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (a AddKeysToAgentOption) Normalize() (AddKeysToAgentOption, error) {
	switch string(a) {
	case boolNo, boolFalse:
		return AddKeysToAgentNo, nil
	case boolYes, boolTrue:
		return AddKeysToAgentYes, nil
	case optAsk:
		return AddKeysToAgentAsk, nil
	case optConfirm:
		return AddKeysToAgentConfirm, nil
	}

	if a.HasInterval() {
		return a, nil
	}

	return "", fmt.Errorf("%w: invalid value %q for AddKeysToAgent", errInvalidValue, a)
}

// ControlMasterOption is an extended boolean option that can be set to yes, no, ask, auto or autoask.
type ControlMasterOption string

const (
	ControlMasterNo      = ControlMasterOption(boolNo)
	ControlMasterYes     = ControlMasterOption(boolYes)
	ControlMasterAsk     = ControlMasterOption(optAsk)
	ControlMasterAuto    = ControlMasterOption(optAuto)
	ControlMasterAutoAsk = ControlMasterOption(optAutoAsk)
)

var ControlMasterOptions = map[string]ControlMasterOption{
	boolNo:     ControlMasterNo,
	boolFalse:  ControlMasterNo,
	boolYes:    ControlMasterYes,
	boolTrue:   ControlMasterYes,
	optAsk:     ControlMasterAsk,
	optAuto:    ControlMasterAuto,
	optAutoAsk: ControlMasterAutoAsk,
}

// IsTrue returns true if the option is set to yes.
func (c ControlMasterOption) IsTrue() bool {
	return c == ControlMasterYes
}

// IsFalse returns true if the option is set to no.
func (c ControlMasterOption) IsFalse() bool {
	return c == ControlMasterNo
}

// IsAsk returns true if the option is set to ask.
func (c ControlMasterOption) IsAsk() bool {
	return c == ControlMasterAsk
}

// IsAuto returns true if the option is set to auto.
func (c ControlMasterOption) IsAuto() bool {
	return c == ControlMasterAuto
}

// IsAutoAsk returns true if the option is set to autoask.
func (c ControlMasterOption) IsAutoAsk() bool {
	return c == ControlMasterAutoAsk
}

// String returns the string representation of the option.
func (c ControlMasterOption) String() string {
	return string(c)
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (c ControlMasterOption) Normalize() (ControlMasterOption, error) {
	if nv, ok := ControlMasterOptions[string(c)]; ok {
		return nv, nil
	}

	return "", fmt.Errorf("%w: invalid value %q for ControlMaster", errInvalidValue, c)
}

// ControlPersistOption is an extended boolean option that can be set to yes, no or a time interval.
type ControlPersistOption string

const (
	ControlPersistOptionNo  = ControlPersistOption(boolNo)
	ControlPersistOptionYes = ControlPersistOption(boolYes)
)

// IsTrue returns true if the option is set to yes or 0.
func (c ControlPersistOption) IsTrue() bool {
	return c == ControlPersistOptionYes || c == "0"
}

// IsFalse returns true if the option is set to no.
func (c ControlPersistOption) IsFalse() bool {
	return c == ControlPersistOptionNo
}

// HasInterval returns true if the option has an interval set.
func (c ControlPersistOption) HasInterval() bool {
	return c[0] >= '0' && c[0] <= '9'
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (c ControlPersistOption) Normalize() (ControlPersistOption, error) {
	switch string(c) {
	case boolNo, boolFalse:
		return ControlPersistOptionNo, nil
	case boolYes, boolTrue:
		return ControlPersistOptionYes, nil
	}

	if c.HasInterval() {
		return c, nil
	}

	return "", fmt.Errorf("%w: invalid value %q for ControlPersist", errInvalidValue, c)
}

// Interval returns the time interval set in the option. If the option does not have an interval an error is returned.
func (c ControlPersistOption) Interval() (time.Duration, error) {
	if !c.HasInterval() {
		return 0, errNoInterval
	}
	d, err := time.ParseDuration(AddIntervalSuffix(string(c)))
	if err != nil {
		return 0, fmt.Errorf("invalid interval: %w", err)
	}
	return d, nil
}

// String returns the string representation of the option.
func (c ControlPersistOption) String() string {
	return string(c)
}

// FingerprintHashOption is an option that can be set to md5 or sha256.
type FingerprintHashOption string

const (
	FingerprintHashMD5    = FingerprintHashOption("MD5")
	FingerprintHashSHA256 = FingerprintHashOption("SHA256")
)

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (f FingerprintHashOption) Normalize() (FingerprintHashOption, error) {
	switch strings.ToLower(string(f)) {
	case "md5":
		return FingerprintHashMD5, nil
	case "sha256":
		return FingerprintHashSHA256, nil
	default:
		return "", fmt.Errorf("%w: invalid value %q for FingerprintHash", errInvalidValue, f)
	}
}

// String returns the string representation of the option.
func (f FingerprintHashOption) String() string {
	return string(f)
}

// ForwardAgentOption is an extended boolean option that can be set to yes, no or a path to an agent socket
// or the name of an environment variable prefixed with a dollar sign.
type ForwardAgentOption string

const (
	ForwardAgentNo  = ForwardAgentOption(boolNo)
	ForwardAgentYes = ForwardAgentOption(boolYes)
)

// IsTrue returns true if the option is set to yes.
func (f ForwardAgentOption) IsTrue() bool {
	return f == ForwardAgentYes
}

// IsFalse returns true if the option is set to no.
func (f ForwardAgentOption) IsFalse() bool {
	return f == ForwardAgentNo
}

// IsSocket returns true if the option is non-empty and not a boolean value.
func (f ForwardAgentOption) IsSocket() bool {
	return !f.IsTrue() && !f.IsFalse() && f != ""
}

// Socket returns the path to the agent socket or the value of the environment variable.
func (f ForwardAgentOption) Socket() string {
	if f.IsTrue() || f.IsFalse() {
		return ""
	}
	str := string(f)
	if strings.HasPrefix(str, "$") {
		return os.ExpandEnv(str)
	}
	if strings.HasPrefix(str, "~/") {
		return userhome() + str[1:]
	}
	return str
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (f ForwardAgentOption) Normalize() (ForwardAgentOption, error) {
	switch string(f) {
	case boolNo, boolFalse:
		return ForwardAgentNo, nil
	case boolYes, boolTrue:
		return ForwardAgentYes, nil
	}

	if f != "" {
		return f, nil
	}

	return "", fmt.Errorf("%w: invalid value %q for ForwardAgent", errInvalidValue, f)
}

// String returns the string representation of the option.
func (f ForwardAgentOption) String() string {
	return string(f)
}

// IdentityAgentOption is an option that can be set to a path to an agent socket or the name of an environment variable prefixed with a dollar sign.
type IdentityAgentOption string

// String returns the string representation of the option.
func (i IdentityAgentOption) String() string {
	return string(i)
}

// Socket returns the path to the agent socket or the value of the environment variable.
func (i IdentityAgentOption) Socket() string {
	str := string(i)
	if str == "" {
		return ""
	}
	if str == "SSH_AUTH_SOCK" {
		return os.Getenv("SSH_AUTH_SOCK")
	}
	if strings.HasPrefix(str, "$") {
		return os.ExpandEnv(str)
	}
	if strings.HasPrefix(str, "~/") {
		return userhome() + str[1:]
	}
	return str
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
// The value is always valid but the function is provided for consistency.
func (i IdentityAgentOption) Normalize() (IdentityAgentOption, error) {
	return i, nil
}

// ObscureKeystrokeTimingOption is an extended boolean option that can be set to yes, no or interval:ms.
type ObscureKeystrokeTimingOption string

const (
	ObscureKeystrokeTimingOptionNo  = ObscureKeystrokeTimingOption(boolNo)
	ObscureKeystrokeTimingOptionYes = ObscureKeystrokeTimingOption(boolYes)
)

// IsTrue returns true if the option is set to yes.
func (o ObscureKeystrokeTimingOption) IsTrue() bool {
	return o == ObscureKeystrokeTimingOptionYes
}

// IsFalse returns true if the option is set to no.
func (o ObscureKeystrokeTimingOption) IsFalse() bool {
	return o == ObscureKeystrokeTimingOptionNo
}

// String returns the string representation of the option.
func (o ObscureKeystrokeTimingOption) String() string {
	return string(o)
}

// HasInterval returns true if the option has an interval set.
func (o ObscureKeystrokeTimingOption) HasInterval() bool {
	_, err := o.Interval()
	return err == nil
}

// Interval returns the time interval set in the option. If the option does not have an interval an error is returned.
func (o ObscureKeystrokeTimingOption) Interval() (time.Duration, error) {
	if !strings.HasPrefix(string(o), "interval:") {
		return 0, errNoInterval
	}
	i, err := strconv.Atoi(string(o)[9:])
	if err != nil {
		return 0, fmt.Errorf("invalid interval: %w", err)
	}
	return time.Duration(i) * time.Millisecond, nil
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (o ObscureKeystrokeTimingOption) Normalize() (ObscureKeystrokeTimingOption, error) {
	switch string(o) {
	case boolNo, boolFalse:
		return ObscureKeystrokeTimingOptionNo, nil
	case boolYes, boolTrue:
		return ObscureKeystrokeTimingOptionYes, nil
	}

	if o.HasInterval() {
		return o, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for ObscureKeystrokeTiming", errInvalidValue, o)
}

// Adds an "s" suffix to a duration string if it ends with a number.
func AddIntervalSuffix(s string) string {
	if s[len(s)-1] >= '0' && s[len(s)-1] <= '9' {
		return s + "s"
	}
	return s
}

// IPQoSOption is the type for the IPQoS option.
type IPQoSOption string

const (
	IPQoSOptionAf11        = IPQoSOption("af11")
	IPQoSOptionAf12        = IPQoSOption("af12")
	IPQoSOptionAf13        = IPQoSOption("af13")
	IPQoSOptionAf21        = IPQoSOption("af21")
	IPQoSOptionAf22        = IPQoSOption("af22")
	IPQoSOptionAf23        = IPQoSOption("af23")
	IPQoSOptionAf31        = IPQoSOption("af31")
	IPQoSOptionAf32        = IPQoSOption("af32")
	IPQoSOptionAf33        = IPQoSOption("af33")
	IPQoSOptionAf41        = IPQoSOption("af41")
	IPQoSOptionAf42        = IPQoSOption("af42")
	IPQoSOptionAf43        = IPQoSOption("af43")
	IPQoSOptionCs0         = IPQoSOption("cs0")
	IPQoSOptionCs1         = IPQoSOption("cs1")
	IPQoSOptionCs2         = IPQoSOption("cs2")
	IPQoSOptionCs3         = IPQoSOption("cs3")
	IPQoSOptionCs4         = IPQoSOption("cs4")
	IPQoSOptionCs5         = IPQoSOption("cs5")
	IPQoSOptionCs6         = IPQoSOption("cs6")
	IPQoSOptionCs7         = IPQoSOption("cs7")
	IPQoSOptionEf          = IPQoSOption("ef")
	IPQoSOptionLe          = IPQoSOption("le")
	IPQoSOptionLowDelay    = IPQoSOption("lowdelay")
	IPQoSOptionThroughput  = IPQoSOption("throughput")
	IPQoSOptionReliability = IPQoSOption("reliability")
	IPQoSOptionNone        = IPQoSOption(optNone)
)

var IPQoSOptions = map[string]IPQoSOption{
	"af11":        IPQoSOptionAf11,
	"af12":        IPQoSOptionAf12,
	"af13":        IPQoSOptionAf13,
	"af21":        IPQoSOptionAf21,
	"af22":        IPQoSOptionAf22,
	"af23":        IPQoSOptionAf23,
	"af31":        IPQoSOptionAf31,
	"af32":        IPQoSOptionAf32,
	"af33":        IPQoSOptionAf33,
	"af41":        IPQoSOptionAf41,
	"af42":        IPQoSOptionAf42,
	"af43":        IPQoSOptionAf43,
	"cs0":         IPQoSOptionCs0,
	"cs1":         IPQoSOptionCs1,
	"cs2":         IPQoSOptionCs2,
	"cs3":         IPQoSOptionCs3,
	"cs4":         IPQoSOptionCs4,
	"cs5":         IPQoSOptionCs5,
	"cs6":         IPQoSOptionCs6,
	"cs7":         IPQoSOptionCs7,
	"ef":          IPQoSOptionEf,
	"le":          IPQoSOptionLe,
	"lowdelay":    IPQoSOptionLowDelay,
	"throughput":  IPQoSOptionThroughput,
	"reliability": IPQoSOptionReliability,
	optNone:       IPQoSOptionNone,
}

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (o IPQoSOption) Normalize() (IPQoSOption, error) {
	if _, ok := IPQoSOptions[string(o)]; ok {
		return o, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for IPQoS", errInvalidValue, o)
}

// String returns the string representation of the option.
func (o IPQoSOption) String() string {
	if o == "" {
		return optNone
	}
	return string(o)
}

// PubkeyAuthenticationOption is the type for the PubkeyAuthentication option.
type PubkeyAuthenticationOption string

const (
	PubkeyAuthenticationOptionYes       = PubkeyAuthenticationOption(boolYes)
	PubkeyAuthenticationOptionNo        = PubkeyAuthenticationOption(boolNo)
	PubkeyAuthenticationOptionUnbound   = PubkeyAuthenticationOption("unbound")
	PubkeyAuthenticationOptionHostbound = PubkeyAuthenticationOption("host-bound")
)

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (o PubkeyAuthenticationOption) Normalize() (PubkeyAuthenticationOption, error) {
	switch string(o) {
	case boolYes, boolTrue:
		return PubkeyAuthenticationOptionYes, nil
	case boolNo, boolFalse:
		return PubkeyAuthenticationOptionNo, nil
	case "unbound":
		return PubkeyAuthenticationOptionUnbound, nil
	case "host-bound":
		return PubkeyAuthenticationOptionHostbound, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for PubkeyAuthentication", errInvalidValue, o)
}

// String returns the string representation of the option.
func (o PubkeyAuthenticationOption) String() string {
	return string(o)
}

// IsTrue returns true if the option is true.
func (o PubkeyAuthenticationOption) IsTrue() bool {
	return o == PubkeyAuthenticationOptionYes
}

// IsFalse returns true if the option is false.
func (o PubkeyAuthenticationOption) IsFalse() bool {
	return o == PubkeyAuthenticationOptionNo
}

// RekeyLimitOption is the type for the RekeyLimit option.
type RekeyLimitOption struct {
	MaxData int64
	MaxTime time.Duration
}

// String returns the string representation of the option.
// If MaxData lines up with a K, M, or G suffix, it will be printed as such.
// If MaxTime is under a minute, it will be printed as a number of seconds.
func (o RekeyLimitOption) String() string {
	if o.MaxData == 0 && o.MaxTime == 0 {
		return ""
	}
	sb := &strings.Builder{}
	switch {
	case o.MaxData >= 1024*1024*1024 && o.MaxData%(1024*1024*1024) == 0:
		fmt.Fprintf(sb, "%dG", o.MaxData/(1024*1024*1024))
	case o.MaxData >= 1024*1024 && o.MaxData%(1024*1024) == 0:
		fmt.Fprintf(sb, "%dM", o.MaxData/(1024*1024))
	case o.MaxData >= 1024 && o.MaxData%1024 == 0:
		fmt.Fprintf(sb, "%dK", o.MaxData/1024)
	default:
		sb.WriteString(strconv.FormatInt(o.MaxData, 10))
	}
	if o.MaxTime > 0 {
		sb.WriteByte(' ')
		if o.MaxTime < time.Minute {
			// write seconds as just numbers
			sb.WriteString(strconv.Itoa(int(time.Duration(o.MaxTime.Seconds()))))
		} else {
			sb.WriteString(formatDuration(o.MaxTime))
		}
	}
	return sb.String()
}

func formatDuration(duration time.Duration) string {
	var parts []string

	hours := int(duration.Hours())
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
		duration -= time.Duration(hours) * time.Hour
	}

	minutes := int(duration.Minutes())
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
		duration -= time.Duration(minutes) * time.Minute
	}

	seconds := int(duration.Seconds())
	if seconds > 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}

	return strings.Join(parts, "")
}

// RequestTTYOption is the type for the RequestTTY option.
type RequestTTYOption string

const (
	RequestTTYOptionYes   = RequestTTYOption(boolYes)
	RequestTTYOptionNo    = RequestTTYOption(boolNo)
	RequestTTYOptionForce = RequestTTYOption("force")
	RequestTTYOptionAuto  = RequestTTYOption(optAuto)
)

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (r RequestTTYOption) Normalize() (RequestTTYOption, error) {
	switch string(r) {
	case boolYes, boolTrue:
		return RequestTTYOptionYes, nil
	case boolNo, boolFalse:
		return RequestTTYOptionNo, nil
	case "force":
		return RequestTTYOptionForce, nil
	case optAuto:
		return RequestTTYOptionAuto, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for RequestTTY", errInvalidValue, r)
}

// IsTrue returns true if the option is true.
func (r RequestTTYOption) IsTrue() bool {
	return r == RequestTTYOptionYes
}

// IsFalse returns true if the option is false.
func (r RequestTTYOption) IsFalse() bool {
	return r == RequestTTYOptionNo
}

// String returns the string representation of the option.
func (r RequestTTYOption) String() string {
	return string(r)
}

// StrictHostKeyCheckingOption is the type for the StrictHostKeyChecking option.
type StrictHostKeyCheckingOption string

const (
	StrictHostKeyCheckingOptionYes       = StrictHostKeyCheckingOption(boolYes)
	StrictHostKeyCheckingOptionNo        = StrictHostKeyCheckingOption(boolNo)
	StrictHostKeyCheckingOptionAcceptNew = StrictHostKeyCheckingOption("accept-new")
	StrictHostKeyCheckingOptionAsk       = StrictHostKeyCheckingOption(optAsk)
)

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (s StrictHostKeyCheckingOption) Normalize() (StrictHostKeyCheckingOption, error) {
	switch string(s) {
	case boolYes, boolTrue:
		return StrictHostKeyCheckingOptionYes, nil
	case boolNo, boolFalse, "off":
		return StrictHostKeyCheckingOptionNo, nil
	case "accept-new":
		return StrictHostKeyCheckingOptionAcceptNew, nil
	case optAsk:
		return StrictHostKeyCheckingOptionAsk, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for StrictHostKeyChecking", errInvalidValue, s)
}

// IsTrue returns true if the option is true.
func (s StrictHostKeyCheckingOption) IsTrue() bool {
	return s == StrictHostKeyCheckingOptionYes
}

// IsFalse returns true if the option is false.
func (s StrictHostKeyCheckingOption) IsFalse() bool {
	return s == StrictHostKeyCheckingOptionNo
}

// IsAsk returns true if the option is "ask".
func (s StrictHostKeyCheckingOption) IsAsk() bool {
	return s == StrictHostKeyCheckingOptionAsk
}

// IsAcceptNew returns true if the option is "accept-new".
func (s StrictHostKeyCheckingOption) IsAcceptNew() bool {
	return s == StrictHostKeyCheckingOptionAcceptNew
}

// String returns the string representation of the option.
func (s StrictHostKeyCheckingOption) String() string {
	return string(s)
}

// TunnelOption is the type for the Tunnel option.
type TunnelOption string

const (
	TunnelOptionYes          = TunnelOption(boolYes)
	TunnelOptionNo           = TunnelOption(boolNo)
	TunnelOptionPointToPoint = TunnelOption("point-to-point")
	TunnelOptionEthernet     = TunnelOption("ethernet")
)

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (t TunnelOption) Normalize() (TunnelOption, error) {
	switch string(t) {
	case boolYes, boolTrue:
		return TunnelOptionYes, nil
	case boolNo, boolFalse:
		return TunnelOptionNo, nil
	case "point-to-point":
		return TunnelOptionPointToPoint, nil
	case "ethernet":
		return TunnelOptionEthernet, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for Tunnel", errInvalidValue, t)
}

// IsTrue returns true if the option is true.
func (t TunnelOption) IsTrue() bool {
	return t == TunnelOptionYes
}

// IsFalse returns true if the option is false.
func (t TunnelOption) IsFalse() bool {
	return t == TunnelOptionNo
}

// String returns the string representation of the option.
func (t TunnelOption) String() string {
	return string(t)
}

// UpdateHostKeysOption is the type for the UpdateHostKeys option.
type UpdateHostKeysOption string

const (
	UpdateHostKeysOptionYes = UpdateHostKeysOption(boolYes)
	UpdateHostKeysOptionNo  = UpdateHostKeysOption(boolNo)
	UpdateHostKeysOptionAsk = UpdateHostKeysOption(optAsk)
)

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (u UpdateHostKeysOption) Normalize() (UpdateHostKeysOption, error) {
	switch string(u) {
	case boolYes, boolTrue:
		return UpdateHostKeysOptionYes, nil
	case boolNo, boolFalse:
		return UpdateHostKeysOptionNo, nil
	case optAsk:
		return UpdateHostKeysOptionAsk, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for UpdateHostKeys", errInvalidValue, u)
}

// IsTrue returns true if the option is true.
func (u UpdateHostKeysOption) IsTrue() bool {
	return u == UpdateHostKeysOptionYes
}

// IsFalse returns true if the option is false.
func (u UpdateHostKeysOption) IsFalse() bool {
	return u == UpdateHostKeysOptionNo
}

// IsAsk returns true if the option is "ask".
func (u UpdateHostKeysOption) IsAsk() bool {
	return u == UpdateHostKeysOptionAsk
}

// String returns the string representation of the option.
func (u UpdateHostKeysOption) String() string {
	return string(u)
}

// VerifyHostKeyDNSOption is the type for the UpdateHostKeys option.
type VerifyHostKeyDNSOption string

const (
	VerifyHostKeyDNSOptionYes = VerifyHostKeyDNSOption(boolYes)
	VerifyHostKeyDNSOptionNo  = VerifyHostKeyDNSOption(boolNo)
	VerifyHostKeyDNSOptionAsk = VerifyHostKeyDNSOption(optAsk)
)

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (u VerifyHostKeyDNSOption) Normalize() (VerifyHostKeyDNSOption, error) {
	switch string(u) {
	case boolYes, boolTrue:
		return VerifyHostKeyDNSOptionYes, nil
	case boolNo, boolFalse:
		return VerifyHostKeyDNSOptionNo, nil
	case optAsk:
		return VerifyHostKeyDNSOptionAsk, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for UpdateHostKeys", errInvalidValue, u)
}

// IsTrue returns true if the option is true.
func (u VerifyHostKeyDNSOption) IsTrue() bool {
	return u == VerifyHostKeyDNSOptionYes
}

// IsFalse returns true if the option is false.
func (u VerifyHostKeyDNSOption) IsFalse() bool {
	return u == VerifyHostKeyDNSOptionNo
}

// IsAsk returns true if the option is "ask".
func (u VerifyHostKeyDNSOption) IsAsk() bool {
	return u == VerifyHostKeyDNSOptionAsk
}

// String returns the string representation of the option.
func (u VerifyHostKeyDNSOption) String() string {
	return string(u)
}

// EscapeCharOption is the type for the EscapeChar option.
type EscapeCharOption string

const (
	EscapeCharOptionNone    = EscapeCharOption("none")
	EscapeCharOptionDefault = EscapeCharOption("~")
)

// Normalize returns the normalized value of the option or an error if the value is invalid.
func (e EscapeCharOption) Normalize() (EscapeCharOption, error) {
	if e == "" || e == "none" {
		return EscapeCharOptionNone, nil
	}
	if e == "~" {
		return EscapeCharOptionDefault, nil
	}
	if len(e) == 1 || (e[0] == '^' && len(e) == 2) {
		return e, nil
	}
	return "", fmt.Errorf("%w: invalid value %q for EscapeChar", errInvalidValue, e)
}

// String returns the string representation of the option.
func (e EscapeCharOption) String() string {
	return string(e)
}

// IsNone returns true if the option is "none".
func (e EscapeCharOption) IsNone() bool {
	return e == EscapeCharOptionNone
}

// Byte returns the byte representation of the option.
func (e EscapeCharOption) Byte() byte {
	if e == EscapeCharOptionNone {
		return 0
	}

	if len(e) == 1 {
		return e[0]
	}

	if e[0] == '^' && len(e) == 2 {
		return e[1] & 31
	}

	return 0
}
