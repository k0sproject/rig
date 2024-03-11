#!/usr/bin/env bash

RET=0
set -e

color_echo() {
  echo -e "\033[1;31m$*\033[0m"
}

ssh_port() {
	bootloose show "$1" -o json|grep hostPort|grep -oE "[0-9]+"
}

sanity_check() {
  color_echo "- Testing bootloose machine connection"
  make create-host
  echo "* bootloose status"
  bootloose status
  echo "* Docker ps"
  docker ps
  echo "* SSH port: $(ssh_port node0)"
  echo "* Testing stock ssh"
  retry ssh -vvv -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i .ssh/identity -p "$(ssh_port node0)" root@127.0.0.1 echo "test-conn" || return $?
  set +e
  echo "* Testing bootloose ssh"
  bootloose ssh root@node0 echo test-conn | grep -q test-conn
  exit_code=$?
  set -e
  make clean
  RET=$exit_code
}

rig_test_key_from_path() {
  color_echo "- Testing regular keypath and host functions"
  make create-host
  mv .ssh/identity .ssh/identity2
  set +e
  go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath .ssh/identity2 
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_agent_with_public_key() {
  color_echo "- Testing connection using agent and providing a path to public key"
  make create-host
  eval "$(ssh-agent -s)"
  ssh-add .ssh/identity
  rm -f .ssh/identity
  set +e
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath .ssh/identity.pub -connect
  exit_code=$?
  set -e
  kill "$SSH_AGENT_PID"
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  RET=$exit_code
}

rig_test_agent_with_private_key() {
  color_echo "- Testing connection using agent and providing a path to protected private key"
  make create-host KEY_PASSPHRASE=testPhrase
  eval "$(ssh-agent -s)"
  expect -c '
    spawn ssh-add .ssh/identity
    expect "?:"
    send "testPhrase\n"
    expect eof"
  '
  set +e
  # path points to a private key, rig should try to look for the .pub for it 
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath .ssh/identity -connect
  exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  RET=$exit_code
}

rig_test_agent() {
  color_echo "- Testing connection using any key from agent (empty keypath)"
  make create-host
  eval "$(ssh-agent -s)"
  ssh-add .ssh/identity
  rm -f .ssh/identity
  set +e
  ssh-add -l
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath "" -connect
  exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  RET=$exit_code
}

rig_test_agent_and_invalid_key() {
  color_echo "- Testing connection using any key from agent (empty keypath) when there is an invalid key in the agent and filesystem"
  make create-host
  eval "$(ssh-agent -s)"
  ssh-keygen -f .ssh/id_rsa -N ""
  ssh-add .ssh/id_rsa
  ssh-add .ssh/identity
  rm -f .ssh/identity
  set +e
  ssh-add -l
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath "" -connect
  exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  rm -f .ssh/id_rsa
  RET=$exit_code
}

rig_test_ssh_config() {
  color_echo "- Testing getting identity path from ssh config"
  make create-host
  mv .ssh/identity .ssh/identity2
  echo "Host 127.0.0.1" > .ssh/config
  echo "  IdentityFile $(pwd)/.ssh/identity2" >> .ssh/config
  chmod 0600 .ssh/config
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host 127.0.0.1 -port "$(ssh_port node0)" -user root -connect
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_ssh_config_strict() {
  color_echo "- Testing StrictHostkeyChecking=yes in ssh config"
  make create-host
  port="$(ssh_port node0)"
  echo "Host testhost" > .ssh/config
  echo "  User root" >> .ssh/config
  echo "  HostName 127.0.0.1" >> .ssh/config
  echo "  Port ${port}" >> .ssh/config
  echo "  IdentityFile $(pwd)/.ssh/identity" >> .ssh/config
  echo "  UserKnownHostsFile $(pwd)/.ssh/known" >> .ssh/config
  echo "  StrictHostKeyChecking yes" >> .ssh/config
  cat .ssh/config
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host testhost -connect
  exit_code=$?
  set -e
  if [ $exit_code -ne 0 ]; then
    echo "  * Failed first checkpoint"
    RET=1
    return
  fi
  echo "  * Passed first checkpoint"
  cat .ssh/known
  # modify the known hosts file to make it mismatch
  echo "[127.0.0.1]:$port ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBBgejI9UJnRY/i4HNM/os57oFcRjE77gEbVfUkuGr5NRh3N7XxUnnBKdzrAiQNPttUjKmUm92BN7nCUxbwsoSPw=" > .ssh/known
  echo "[127.0.0.1]:$port ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBGZKwBdFeIPlDWe7otNy4E2Im8+GnQtsukJ5dIuzDGb" >> .ssh/known
  cat .ssh/known
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host testhost -connect
  exit_code=$?
  set -e

  if [ $exit_code -eq 0 ]; then
    echo "  * Failed second checkpoint"
    # success is a failure
    RET=1
    return
  fi
  echo "  * Passed second checkpoint"
}

rig_test_ssh_config_no_strict() {
  color_echo "- Testing StrictHostkeyChecking=no in ssh config"
  make create-host
  port="$(ssh_port node0)"
  echo "Host testhost" > .ssh/config
  echo "  User root" >> .ssh/config
  echo "  HostName 127.0.0.1" >> .ssh/config
  echo "  Port ${port}" >> .ssh/config
  echo "  UserKnownHostsFile $(pwd)/.ssh/known" >> .ssh/config
  echo "  StrictHostKeyChecking no" >> .ssh/config
  cat .ssh/config
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host testhost -connect
  exit_code=$?
  set -e
  if [ $? -ne 0 ]; then
    RET=1
    return
  fi
  # modify the known hosts file to make it mismatch
  echo "[127.0.0.1]:$port ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBBgejI9UJnRY/i4HNM/os57oFcRjE77gEbVfUkuGr5NRh3N7XxUnnBKdzrAiQNPttUjKmUm92BN7nCUxbwsoSPw=" > .ssh/known
  echo "[127.0.0.1]:$port ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBGZKwBdFeIPlDWe7otNy4E2Im8+GnQtsukJ5dIuzDGb" >> .ssh/known
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host testhost -connect
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_key_from_memory() {
  color_echo "- Testing connecting using a key from string"
  make create-host
  mv .ssh/identity .ssh/identity2
  set +e
  go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-private-key "$(cat .ssh/identity2)" -connect
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_key_from_default_location() {
  color_echo "- Testing keypath from default location"
  make create-host
  mv .ssh/identity .ssh/id_ecdsa
  set +e
  HOME=$(pwd) go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -connect
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_regular_user() {
  color_echo "- Testing regular user"
  make create-host
  sshPort=$(ssh_port node0)

  set -- -T -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i .ssh/identity -p "$sshPort"
  retry ssh "$@" root@127.0.0.1 true || {
    RET=$?
    color_echo failed to SSH into machine >&2
    return 0
  }

  ssh "$@" root@127.0.0.1 sh -euxC - <<EOF
    groupadd --system rig-wheel
    useradd -d /var/lib/rigtest-user -G rig-wheel -p '*' rigtest-user
    mkdir -p /var/lib/rigtest-user/
    cp -r /root/.ssh /var/lib/rigtest-user/.
    chown -R rigtest-user:rigtest-user /var/lib/rigtest-user/
    [ ! -d /etc/sudoers.d/ ] || {
      echo '%rig-wheel ALL=(ALL)NOPASSWD:ALL' >/etc/sudoers.d/rig-wheel
      chmod 0440 /etc/sudoers.d/rig-wheel
    }
    [ ! -d /etc/doas.d/ ] || {
      echo 'permit nopass :rig-wheel' >/etc/doas.d/rig-wheel.conf
      chmod 0440 /etc/doas.d/rig-wheel.conf
    }
EOF
  RET=$?
  [ $RET -eq 0 ] || {
    color_echo failed to provision new user rigtest-user >&2
    return 0
  }

  ssh "$@" rigtest-user@127.0.0.1 true || {
    RET=$?
    color_echo failed to SSH into machine as rigtest-user >&2
    return 0
  }

  HOME="$(pwd)" go test -v ./ -args -host 127.0.0.1 -port "$sshPort" -user rigtest-user -ssh-keypath .ssh/identity
}

rig_test_openssh_client() {
  color_echo "- Testing openssh client protocol"
  make create-host
  echo "Host testhost" > .ssh/config
  echo "  HostName 127.0.0.1" >> .ssh/config
  echo "  Port $(ssh_port node0)" >> .ssh/config
  echo "  User root" >> .ssh/config
  echo "  IdentityFile $(pwd)/.ssh/identity" >> .ssh/config
  echo "  UserKnownHostsFile /dev/null" >> .ssh/config
  echo "  StrictHostKeyChecking no" >> .ssh/config
  cat .ssh/config
  set +e
  go test -v ./ -args -ssh-configpath .ssh/config -host testhost -protocol openssh -user ""
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_openssh_client_no_multiplex() {
  color_echo "- Testing openssh client protocol without ssh multiplexing"
  make create-host
  echo "Host testhost" > .ssh/config
  echo "  HostName 127.0.0.1" >> .ssh/config
  echo "  Port $(ssh_port node0)" >> .ssh/config
  echo "  User root" >> .ssh/config
  echo "  IdentityFile $(pwd)/.ssh/identity" >> .ssh/config
  echo "  UserKnownHostsFile /dev/null" >> .ssh/config
  echo "  StrictHostKeyChecking no" >> .ssh/config
  cat .ssh/config
  set +e
  go test -v ./ -args -ssh-configpath .ssh/config -host testhost -protocol openssh -user "" -openssh-multiplex=false
  exit_code=$?
  set -e
  RET=$exit_code
}


retry() {
  local i
  for i in 1 2 3 4 5; do
    ! "$@" || return 0
    sleep $i
  done
  "$@"
}

if [ -z "$FOCUS" ] && ! sanity_check; then
  color_echo Sanity check failed >&2
  exit 1
fi

for test in $(declare -F|grep rig_test_|cut -d" " -f3); do
  if [ "$FOCUS" != "" ] && [ "$FOCUS" != "$test" ]; then
    continue
  fi
  make clean
  color_echo "\n###########################################################"
  RET=0
  $test || RET=$?
  if [ $RET -ne 0 ]; then
    color_echo "Test $test failed" >&2
    exit 1
  fi
  echo -e "\n\n\n"
done
