package sshconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TODO This file contains tests for unexported functions and structs.
// The tests can be modified to use the exported API.

func TestTreeParser(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"spaces",
			`
				GlobalKnownHostsFile /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
				Host example.com
					IdentityFile ~/.ssh/id_new # comment
				Host foo.example.com
					StrictHostKeyChecking yes
			    Weird "=foo #foo"
			`,
		},
		{"equals",
			`
				GlobalKnownHostsFile=/etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
				Host example.com
					IdentityFile=~/.ssh/id_new # comment
				Host=foo.example.com
					StrictHostKeyChecking=yes
			    Weird="=foo #foo"
			`,
		},
		{"equals with spaces",
			`
				GlobalKnownHostsFile = /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
				Host example.com
					IdentityFile = ~/.ssh/id_new # comment
				Host = foo.example.com
					StrictHostKeyChecking=yes
			    Weird = "=foo #foo"
			`,
		},
		{"tabs",
			strings.ReplaceAll(strings.ReplaceAll(`
				GlobalKnownHostsFile=/etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
				Host=example.com
				IdentityFile=~/.ssh/id_new # comment
				Host=foo.example.com
				StrictHostKeyChecking=yes
			  Weird='?foo #foo'
			`, "=", "\t"), "?", "="),
		},
		{"crazy spaces and equals",
			`
				GlobalKnownHostsFile     =      /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
				Host                     =      example.com
					IdentityFile           =      ~/.ssh/id_new # comment
				Host                     =      foo.example.com
					StrictHostKeyChecking  =      yes
			    Weird                  =      "=foo #foo"
			`,
		},
		{"crazy spaces",
			`
				GlobalKnownHostsFile            /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
				Host                            example.com
					IdentityFile                  ~/.ssh/id_new # comment
				Host                            foo.example.com
					StrictHostKeyChecking         yes
			    Weird                         '=foo #foo'
			`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := newTreeParser(strings.NewReader(tc.content))
			iter, err := parser.Parse()
			require.NoError(t, err)
			sawFields := make(map[string][]string)
			for iter.Next() {
				if _, ok := sawFields[iter.Key()]; !ok {
					sawFields[iter.Key()] = iter.Values()
				}
				if iter.Key() == "host" && iter.Values()[0] == "example.com" {
					iter.Skip()
				}
			}
			saw, ok := sawFields["globalknownhostsfile"]
			assert.True(t, ok)
			assert.Equal(t, []string{"/etc/ssh/ssh_known_hosts", "/etc/ssh/ssh_known_hosts2"}, saw)

			saw, ok = sawFields["identityfile"]
			assert.True(t, ok)
			assert.NotEqual(t, []string{"~/.ssh/id_new"}, saw, "Host example.com block should have been skipped")

			saw, ok = sawFields["host"]
			assert.True(t, ok)
			assert.Equal(t, []string{"example.com"}, saw) // the iter-loop only captures the first occurence

			saw, ok = sawFields["stricthostkeychecking"]
			assert.True(t, ok)
			assert.Equal(t, []string{"yes"}, saw)

			saw, ok = sawFields["weird"]
			assert.True(t, ok)
			assert.Equal(t, []string{"=foo #foo"}, saw)
		})
	}
}
