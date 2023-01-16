#!/bin/bash

set -e

color_echo() {
  echo -e "\033[1;31m$@\033[0m"
}

ssh_port() {
	footloose show $1 -o json|grep hostPort|grep -oE "[0-9]+"
}

sanity_check() {
  color_echo "- Testing footloose machine connection"
  make create-host
  echo "* Footloose status"
  footloose status
  echo "* Docker ps"
  docker ps
  echo "* SSH port: $(ssh_port node0)"
  echo "* Testing stock ssh"
  ssh -vvv -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i .ssh/identity -p $(ssh_port node0) root@127.0.0.1 echo "test-conn"
  set +e
  echo "* Testing footloose ssh"
  footloose ssh root@node0 echo test-conn | grep -q test-conn
  local exit_code=$?
  set -e
  make clean
  return $exit_code
}


rig_test_agent_with_public_key() {
  color_echo "- Testing connection using agent and providing a path to public key"
  make create-host
  eval $(ssh-agent -s)
  ssh-add .ssh/identity
  rm -f .ssh/identity
  set +e
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK ./rigtest -host 127.0.0.1:$(ssh_port node0) -user root -keypath .ssh/identity.pub
  local exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  return $exit_code
}

rig_test_agent_with_private_key() {
  color_echo "- Testing connection using agent and providing a path to protected private key"
  make create-host KEY_PASSPHRASE=testPhrase
  eval $(ssh-agent -s)
  expect -c '
    spawn ssh-add .ssh/identity
    expect "?:"
    send "testPhrase\n"
    expect eof"
  '
  set +e
  # path points to a private key, rig should try to look for the .pub for it 
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK ./rigtest -host 127.0.0.1:$(ssh_port node0) -user root -keypath .ssh/identity
  local exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  return $exit_code
}

rig_test_agent() {
  color_echo "- Testing connection using any key from agent (empty keypath)"
  make create-host
  eval $(ssh-agent -s)
  ssh-add .ssh/identity
  rm -f .ssh/identity
  set +e
  ssh-add -l
  HOME=. SSH_AUTH_SOCK=$SSH_AUTH_SOCK ./rigtest -host 127.0.0.1:$(ssh_port node0) -user root -keypath ""
  local exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  return $exit_code
}

rig_test_ssh_config() {
  color_echo "- Testing getting identity path from ssh config"
  make create-host
  mv .ssh/identity .ssh/identity2
  echo "Host 127.0.0.1:$(ssh_port node0)" > .ssh/config
  echo "  IdentityFile .ssh/identity2" >> .ssh/config
  set +e
  HOME=. SSH_CONFIG=.ssh/config ./rigtest -host 127.0.0.1:$(ssh_port node0) -user root
  local exit_code=$?
  set -e
  return $exit_code
}

rig_test_ssh_config_strict() {
  color_echo "- Testing StrictHostkeyChecking=yes in ssh config"
  make create-host
  local addr="127.0.0.1:$(ssh_port node0)"
  echo "Host ${addr}" > .ssh/config
  echo "  UserKnownHostsFile $(pwd)/.ssh/known" >> .ssh/config
  set +e
  HOME=. SSH_CONFIG=.ssh/config ./rigtest -host "${addr}" -user root
  if [ $? -ne 0 ]; then
    return 1
  fi
  # modify the known hosts file to make it mismatch
  echo "${addr} ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBBgejI9UJnRY/i4HNM/os57oFcRjE77gEbVfUkuGr5NRh3N7XxUnnBKdzrAiQNPttUjKmUm92BN7nCUxbwsoSPw=" > .ssh/known
  HOME=. SSH_CONFIG=.ssh/config ./rigtest -host "${addr}" -user root
  local exit_code=$?
  rm -f .ssh/known
  set -e

  if [ $exit_code -eq 0 ]; then
    # success is a failure
    return 1
  fi

  return 0
}

rig_test_ssh_config_no_strict() {
  color_echo "- Testing StrictHostkeyChecking=no in ssh config"
  make create-host
  local addr="127.0.0.1:$(ssh_port node0)"
  echo "Host ${addr}" > .ssh/config
  echo "  UserKnownHostsFile $(pwd)/.ssh/known" >> .ssh/config
  echo "  StrictHostKeyChecking no" >> .ssh/config
  set +e
  HOME=. SSH_CONFIG=.ssh/config ./rigtest -host "${addr}" -user root
  if [ $? -ne 0 ]; then
    return 1
  fi
  # modify the known hosts file to make it mismatch
  echo "${addr} ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBBgejI9UJnRY/i4HNM/os57oFcRjE77gEbVfUkuGr5NRh3N7XxUnnBKdzrAiQNPttUjKmUm92BN7nCUxbwsoSPw=" > .ssh/known
  HOME=. SSH_CONFIG=.ssh/config ./rigtest -host "${addr}" -user root
  local exit_code=$?
  rm -f .ssh/known
  set -e
  return $exit_code
}


rig_test_key_from_path() {
  color_echo "- Testing regular keypath"
  make create-host
  mv .ssh/identity .ssh/identity2
  set +e
  ./rigtest -host 127.0.0.1:$(ssh_port node0) -user root -keypath .ssh/identity2
  local exit_code=$?
  set -e
  return $exit_code
}

rig_test_protected_key_from_path() {
  color_echo "- Testing regular keypath to encrypted key, two hosts"
  make create-host KEY_PASSPHRASE=testPhrase REPLICAS=2
  set +e
  ssh_port node0 > .ssh/port_A
  ssh_port node1 > .ssh/port_B
  expect -c '
  
    set fp [open .ssh/port_A r]
    set PORTA [read -nonewline $fp]
    close $fp
    set fp [open .ssh/port_B r]
    set PORTB [read -nonewline $fp]
    close $fp

    spawn ./rigtest -host 127.0.0.1:$PORTA,127.0.0.1:$PORTB -user root -keypath .ssh/identity -askpass true
    expect "Password:"
    send "testPhrase\n"
    expect eof"
  ' $port1 $port2
  local exit_code=$?
  set -e
  rm footloose.yaml
  make delete-host REPLICAS=2
  return $exit_code
}

if ! sanity_check; then
  echo "Sanity check failed"
  exit 1
fi

for test in $(declare -F|grep rig_test_|cut -d" " -f3); do
  if [ "$FOCUS" != "" ] && [ "$FOCUS" != "$test" ]; then
    continue
  fi
  make clean
  make rigtest
  color_echo "\n###########################################################"
  $test
  echo -e "\n\n\n"
done
