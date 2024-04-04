package sshconfig

var defaultGlobalConfigPath = func() string {
	return "/etc/ssh/ssh_config"
}

// sshDefaultConfig is the default configuration for an SSH client.
// this is obtained via "ssh -G" on a mac without any ssh config
// files. the output is sorted and the fields with key
// "identityfile" are joined into a single line.
//
// note that some of the boolean values are displayed as "true"/"false"
// instead of "yes"/"no".
const sshDefaultConfig = `
addkeystoagent false
addressfamily any
applemultipath no
batchmode no
canonicaldomains none
canonicalizePermittedcnames none
canonicalizefallbacklocal yes
canonicalizehostname false
canonicalizemaxdots 1
casignaturealgorithms ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,rsa-sha2-512,rsa-sha2-256
checkhostip no
ciphers chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com
clearallforwardings no
compression no
connectionattempts 1
connecttimeout none
controlmaster false
controlpersist no
enableescapecommandline no
enablesshkeysign no
escapechar ~
exitonforwardfailure no
fingerprinthash SHA256
forkafterauthentication no
forwardagent no
forwardx11 no
forwardx11timeout 1200
forwardx11trusted no
gatewayports no
globalknownhostsfile /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
gssapiauthentication no
gssapidelegatecredentials no
hashknownhosts no
hostbasedacceptedalgorithms ssh-ed25519-cert-v01@openssh.com,ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,sk-ssh-ed25519-cert-v01@openssh.com,sk-ecdsa-sha2-nistp256-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,rsa-sha2-512,rsa-sha2-256
hostbasedauthentication no
hostkeyalgorithms ssh-ed25519-cert-v01@openssh.com,ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,sk-ssh-ed25519-cert-v01@openssh.com,sk-ecdsa-sha2-nistp256-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,rsa-sha2-512,rsa-sha2-256
identitiesonly no
identityfile ~/.ssh/id_dsa ~/.ssh/id_ecdsa ~/.ssh/id_ecdsa_sk ~/.ssh/id_ed25519 ~/.ssh/id_ed25519_sk ~/.ssh/id_rsa ~/.ssh/id_xmss
ipqos af21 cs1
kbdinteractiveauthentication yes
kexalgorithms sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256
loglevel INFO
logverbose none
macs umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1
nohostauthenticationforlocalhost no
numberofpasswordprompts 3
passwordauthentication yes
permitlocalcommand no
permitremoteopen any
port 22
proxyusefdpass no
pubkeyacceptedalgorithms ssh-ed25519-cert-v01@openssh.com,ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,sk-ssh-ed25519-cert-v01@openssh.com,sk-ecdsa-sha2-nistp256-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,rsa-sha2-512,rsa-sha2-256
pubkeyauthentication true
rekeylimit 0 0
requesttty auto
requiredrsasize 1024
securitykeyprovider $SSH_SK_PROVIDER
serveralivecountmax 3
serveraliveinterval 30
sessiontype default
stdinnull no
streamlocalbindmask 0177
streamlocalbindunlink no
stricthostkeychecking ask
syslogfacility USER
tcpkeepalive yes
tunnel false
tunneldevice any:any
updatehostkeys true
userknownhostsfile ~/.ssh/known_hosts ~/.ssh/known_hosts2
verifyhostkeydns false
visualhostkey no
xauthlocation /usr/X11R6/bin/xauth
`
