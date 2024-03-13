package sshconfig

import (
	"bufio"
	"bytes"
	"os/exec"
	"strings"
	"sync"
)

var haveOpenSSH = sync.OnceValue(func() bool {
	cmd := exec.Command("ssh", "-V")
	return cmd.Run() == nil
})

func getFromOpenSSH(key string) string {
	if !haveOpenSSH() {
		return ""
	}
	cmd := exec.Command("ssh", "-Q", key)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	var items []string
	for scanner.Scan() {
		row := strings.TrimSpace(scanner.Text())
		if row == "" {
			continue
		}
		if strings.Contains(row, " ") {
			continue
		}
		items = append(items, row)
	}
	if scanner.Err() != nil {
		return ""
	}
	return strings.Join(items, ",")
}

// from openssh 9.4p1, libreSSL 3.3.6.
const (
	defaultCASig        = "ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,rsa-sha2-512,rsa-sha2-256"
	defaultCiphers      = "chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com"
	defaultHostkeyAlgos = "ssh-ed25519-cert-v01@openssh.com,ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,sk-ssh-ed25519-cert-v01@openssh.com,sk-ecdsa-sha2-nistp256-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,rsa-sha2-512,rsa-sha2-256"
	defaultKexAlgos     = "sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256"
	defaultMACs         = "umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1"
)

// defaultList returns the default list of algos for the given key. it tries to get them from the local openssh installation when available.
func defaultList(key string) string {
	if fromSSH := getFromOpenSSH(key); fromSSH != "" {
		return fromSSH
	}
	switch key {
	case "casignaturealgorithms":
		return defaultCASig
	case "ciphers":
		return defaultCiphers
	case "hostbasedacceptedalgorithms":
		return defaultHostkeyAlgos
	case "hostkeyalgorithms":
		return defaultHostkeyAlgos
	case "kexalgorithms":
		return defaultKexAlgos
	case "macs":
		return defaultMACs
	case "pubkeyacceptedalgorithms":
		return defaultHostkeyAlgos
	}
	return ""
}
