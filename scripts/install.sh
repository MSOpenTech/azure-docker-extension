#!/bin/bash

# Author Jeff Mendoza <jeffmendoza@live.com>
#-------------------------------------------------------------------------
# Copyright (c) Microsoft Open Technologies, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#--------------------------------------------------------------------------

set -eu
set -o pipefail
script_dir=$(cd $(dirname $0); pwd)
source $script_dir/dockerlib.sh

signal_transitioning
validate_distro

IFS=$'\n\t'
exec >> $log_file 2>&1

log "Installing Docker..."

if [ $distro == "Ubuntu" ]; then
    wget -qO- https://get.docker.com/ | sh
elif [ $distro == "CoreOS" ]; then
    log "Copy /usr/lib/systemd/system/docker.service --> /etc/systemd/system/"
    cp /usr/lib/systemd/system/docker.service /etc/systemd/system/
fi

# Install should not imply starting of the docker service, however in the Ubuntu install
# case above, running Docker's install script will start the service.
stop_docker

log "Adding user to docker group"
azureuser=$(grep -Eo '<UserName>.+</UserName>' /var/lib/waagent/ovf-env.xml | awk -F'[<>]' '{ print $3 }')
sed -i -r "s/^docker:x:[0-9]+:$/&$azureuser/" /etc/group

log "Done installing Docker"

log "Installing Docker Compose..."

if [ $distro == "CoreOS" ]; then
    COMPOSE_DIR=/opt/bin
    mkdir -p $compose_dir
else
    COMPOSE_DIR=/usr/local/bin
fi

curl -L https://github.com/docker/compose/releases/download/1.2.0/docker-compose-`uname -s`-`uname -m` > $COMPOSE_DIR/docker-compose
chmod +x $COMPOSE_DIR/docker-compose

log "Done installing Docker Compose"

log "Installing certificates"

if [ ! -d $docker_dir ]; then
    log "Creating $docker_dir"
    mkdir $docker_dir
fi

thumb=$(cat $config_path | json_val \
    '["runtimeSettings"][0]["handlerSettings"]["protectedSettingsCertThumbprint"]')
cert=/var/lib/waagent/${thumb}.crt
pkey=/var/lib/waagent/${thumb}.prv
prot=$script_dir/prot.json

cat $config_path | \
    json_val '["runtimeSettings"][0]["handlerSettings"]["protectedSettings"]' | \
    base64 -d | \
    openssl smime  -inform DER -decrypt -recip $cert  -inkey $pkey > \
    $prot

log "Creating certificates in $docker_dir"

cat $prot | json_val '["ca"]' | base64 -d > $docker_dir/ca.pem
cat $prot | json_val '["server-cert"]' | base64 -d > $docker_dir/server-cert.pem
cat $prot | json_val '["server-key"]' | base64 -d > $docker_dir/server-key.pem

rm $prot
chmod 600 $docker_dir/*

log "Done installing certificates"

log "Initializing docker command line arguments"

port=$(cat $config_path | json_val \
    '["runtimeSettings"][0]["handlerSettings"]["publicSettings"]["dockerport"]')

log "Docker port: $port"

if [ $distro == "Ubuntu" ]; then
    log "Setting up /etc/default/docker"
    cat <<EOF > /etc/default/docker
DOCKER_OPTS="--tlsverify --tlscacert=$docker_dir/ca.pem --tlscert=$docker_dir/server-cert.pem --tlskey=$docker_dir/server-key.pem -H=0.0.0.0:$port"
EOF
    update-rc.d docker defaults
elif [ $distro == "CoreOS" ]; then
    log "Setting up /etc/systemd/system/docker.service"
    sed -i "s%ExecStart=.*%ExecStart=/usr/bin/docker --daemon --tlsverify --tlscacert=$docker_dir/ca.pem --tlscert=$docker_dir/server-cert.pem --tlskey=$docker_dir/server-key.pem -H=0.0.0.0:$port%" /etc/systemd/system/docker.service
    systemctl daemon-reload
fi

log "Done initializing docker command line arguments"
