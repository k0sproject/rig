#!/bin/bash

set -e

color_echo() {
  echo -e "\033[1;31m$@\033[0m"
}

ssh_port() {
	footloose show node0 -o json|grep hostPort|grep -oE "[0-9]+"
}

rig_test_agent_with_public_key() {
  color_echo "- Testing connection using agent and providing a path to public key"
  make create-host
  eval $(ssh-agent -s)
  ssh-add .ssh/identity
  rm -f .ssh/identity
  set +e
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK ./rigtest -port $(ssh_port) -user root -keypath .ssh/identity.pub
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
  HOME=. SSH_AUTH_SOCK=$SSH_AUTH_SOCK ./rigtest -port $(ssh_port) -user root -keypath ""
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
  echo "Host 127.0.0.1:$(ssh_port)" > .ssh/config
  echo "  IdentityFile .ssh/identity2" >> .ssh/config
  set +e
  HOME=. SSH_CONFIG=.ssh/config ./rigtest -port $(ssh_port) -user root
  local exit_code=$?
  set -e
  return $exit_code
}

rig_test_key_from_path() {
  color_echo "- Testing regular keypath"
  make create-host
  mv .ssh/identity .ssh/identity2
  set +e
  ./rigtest -port $(ssh_port) -user root -keypath .ssh/identity2
  local exit_code=$?
  set -e
  return $exit_code
}

for test in $(declare -F|grep rig_test_|cut -d" " -f3); do
  make clean
  make rigtest
  color_echo "\n###########################################################"
  $test
  echo -e "\n\n\n"
done
