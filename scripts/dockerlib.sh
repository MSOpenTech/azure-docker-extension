#!/bin/bash

# Author Gabriel Hartmann <gabhart@microsoft.com>
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

distro=$(awk -F'=' '{if($1=="DISTRIB_ID")print $2; }' /etc/*-release)

if [ $distro == "CoreOS" ]; then
    type python >/dev/null 2>&1 || { export PATH=$PATH:/usr/share/oem/python/bin/; }
    type python >/dev/null 2>&1 || { echo >&2 "Python is required but it's not installed."; exit 1; }
fi

json_val() {
    python -c 'import json,sys;obj=json.load(sys.stdin);print obj'$1'';
}

json_dump() {
    python -c 'import json,sys;obj=json.load(sys.stdin);print json.dumps(obj'$1')';
}

yaml_dump() {
    python -c 'import json,yaml,sys;data=json.load(sys.stdin);print yaml.safe_dump(data, default_flow_style=False)'
}

script_dir=$(cd $(dirname $0); pwd)
docker_dir=/etc/docker
log_dir=$(cat $script_dir/../HandlerEnvironment.json | json_val '[0]["handlerEnvironment"]["logFolder"]')
config_dir=$(cat $script_dir/../HandlerEnvironment.json | json_val '[0]["handlerEnvironment"]["configFolder"]')
status_dir=$(cat $script_dir/../HandlerEnvironment.json | json_val '[0]["handlerEnvironment"]["statusFolder"]')

log_file=$log_dir/docker-handler.log
config_file=$(ls $config_dir | grep -E ^[0-9]+.settings$ | sort -n | tail -n 1)
status_file=$(echo $config_file | sed s/settings/status/)

config_path=$config_dir/$config_file
status_path=$status_dir/$status_file

install_only() {
    local only_install="false"
		
    if [ -n "$(cat $config_path | json_val \
        '["runtimeSettings"][0]["handlerSettings"]["publicSettings"]["installonly"]' \
        2>/dev/null )" ]; then
        only_install=$(cat $config_path | json_val \
        '["runtimeSettings"][0]["handlerSettings"]["publicSettings"]["installonly"]')
    fi

    echo $only_install
}

log() {
    local file_name=${0##*/}
    local timestamp=$(date +"%Y-%m-%d %H:%M:%S")
    echo "$timestamp $file_name: $1"
}

validate_distro() {
    if [ $distro == "" ]; then
        log "Error reading DISTRO"
        exit 1
    fi

    if [[ $distro == "CoreOS" || $distro == "Ubuntu" ]]; then
        log "OS $distro is supported."
    else
        log "OS $distro is NOT supported."
        exit 1;
    fi
}

restart_docker() {
    log "Restarting Docker"
    if [ $distro == "Ubuntu" ]; then
        service docker restart
    elif [ $distro == "CoreOS" ]; then
        systemctl restart docker
    fi
}

stop_docker() {
    log "Stopping docker service"
    
    if [ $distro == "Ubuntu" ]; then
        service docker stop
    elif [ $distro == "CoreOS" ]; then
        systemctl stop docker
    fi
}

signal_transitioning() {
    cat $script_dir/running.status.json | sed s/@@DATE@@/$(date -u +%FT%TZ)/ > $status_path
}

signal_success() {
    cat $script_dir/success.status.json | sed s/@@DATE@@/$(date -u +%FT%TZ)/ > $status_path
}
