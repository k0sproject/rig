package sshconfig

import (
	"os"
	"path"
)

var defaultGlobalConfigPath = func() string {
	pd := os.Getenv("PROGRAMDATA")
	if pd == "" {
		pd = "C:/ProgramData"
	}
	return path.Join(pd, "ssh", "ssh_config")
}

// sshDefaultConfig is the default configuration for an SSH client.
// this is obtained via "ssh -G" on a fresh windows machine without
// any ssh config files.
//
// note that some of the boolean values are displayed as "true"/"false"
// instead of "yes"/"no" and the __PROGRAMDATA__ in some of the paths
// in addition to mixed path separators.
const sshDefaultConfig = `
addkeystoagent false
addressfamily any
batchmode no
canonicaldomains
canonicalizefallbacklocal yes
canonicalizehostname false
canonicalizemaxdots 1
casignaturealgorithms ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
challengeresponseauthentication yes
checkhostip yes
ciphers chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com
clearallforwardings no
compression no
connectionattempts 1
connecttimeout none
controlmaster false
controlpersist no
enablesshkeysign no
escapechar ~
exitonforwardfailure no
fingerprinthash SHA256
forwardagent no
forwardx11 no
forwardx11timeout 1200
forwardx11trusted no
gatewayports no
globalknownhostsfile __PROGRAMDATA__/ssh/ssh_known_hosts __PROGRAMDATA__/ssh/ssh_known_hosts2
gssapiauthentication no
gssapidelegatecredentials no
hashknownhosts no
hostbasedauthentication no
hostbasedkeytypes ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
hostkeyalgorithms ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
identitiesonly no
identityfile ~/.ssh/id_dsa ~/.ssh/id_ecdsa ~/.ssh/id_ed25519 ~/.ssh/id_rsa ~/.ssh/id_xmss
ipqos af21 cs1
kbdinteractiveauthentication yes
kexalgorithms curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1
loglevel INFO
macs umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1
nohostauthenticationforlocalhost no
numberofpasswordprompts 3
passwordauthentication yes
permitlocalcommand no
port 22
proxyusefdpass no
pubkeyacceptedkeytypes ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
pubkeyauthentication yes
rekeylimit 0 0
requesttty auto
serveralivecountmax 3
serveraliveinterval 0
streamlocalbindmask 0177
streamlocalbindunlink no
stricthostkeychecking ask
syslogfacility USER
tcpkeepalive yes
tunnel false
tunneldevice any:any
updatehostkeys false
userknownhostsfile ~/.ssh/known_hosts ~/.ssh/known_hosts2
verifyhostkeydns false
visualhostkey no
xauthlocation __PROGRAMDATA__/ssh/bin/xauth
`
