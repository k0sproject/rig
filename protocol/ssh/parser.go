package ssh

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/sshconfig"
)

// ConfigParser is an interface for applying an SSH configuration to an object.
type ConfigParser interface {
	Apply(obj any, hostalias string) error
}

type parserGetter interface {
	Get(path string) (ConfigParser, error)
}

// ParserCache is a sync.OnceValue that creates a new parserCache for caching
// ssh configuration parsers to avoid re-parsing the same configs multiple times.
var ParserCache = sync.OnceValue(func() parserGetter {
	return &parserCache{
		cache:    make(map[string]*sshconfig.Parser),
		errCache: make(map[string]error),
	}
})

type parserCache struct {
	sync.Mutex
	cache    map[string]*sshconfig.Parser
	errCache map[string]error
}

func (c *parserCache) Get(path string) (ConfigParser, error) {
	c.Lock()
	defer c.Unlock()

	if err, ok := c.errCache[path]; ok {
		return nil, err
	}

	if parser, ok := c.cache[path]; ok {
		log.Trace(context.Background(), "ssh config parser cache hit", "path", path)
		return parser, nil
	}
	log.Trace(context.Background(), "ssh config parser cache miss", "path", path)

	if path == "" {
		log.Trace(context.Background(), "creating a default locations ssh config parser")
		parser, err := sshconfig.NewParser(nil)
		if err != nil {
			err = fmt.Errorf("create ssh config parser using system paths: %w", err)
			c.errCache[path] = err
			return nil, err
		}
		c.cache[path] = parser
		return parser, nil
	}

	expanded, err := homedir.Expand(path)
	if err != nil {
		err = fmt.Errorf("expand ssh config path %q: %w", path, err)
		c.errCache[path] = err
		return nil, err
	}

	f, err := os.Open(expanded)
	if err != nil {
		err = fmt.Errorf("open ssh config %q: %w", expanded, err)
		c.errCache[path] = err
		return nil, err
	}
	defer f.Close()

	log.Trace(context.Background(), "creating a ssh config parser", log.KeyFile, expanded)
	parser, err := sshconfig.NewParser(f)
	if err != nil {
		err = fmt.Errorf("parse ssh config %q: %w", expanded, err)
		c.errCache[path] = err
		return nil, err
	}

	c.cache[path] = parser
	return parser, nil
}
