package exec

// Extract separates exec.Options from other sprintf templating args
func Extract(params ...any) ([]Option, []any) {
	var opts []Option
	var args []any
	for _, v := range params {
		switch vv := v.(type) {
		case []any:
			o, a := Extract(vv...)
			opts = append(opts, o...)
			args = append(args, a...)
		case Option:
			opts = append(opts, vv)
		default:
			args = append(args, vv)
		}
	}
	return opts, args
}
