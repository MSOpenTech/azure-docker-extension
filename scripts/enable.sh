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

log "Enabling Docker"
log "Using config: $config_path"

if [[ $(install_only) == "false" ]]; then
    restart_docker
else
    stop_docker 
	signal_success
	exit
fi

if [ -n "$(cat $config_path | json_dump '["runtimeSettings"][0]["handlerSettings"]["publicSettings"]["composeup"]' 2>/dev/null )" ]; then
    compose_up=$(cat $config_path | json_dump '["runtimeSettings"][0]["handlerSettings"]["publicSettings"]["composeup"]')
else
    compose_up="false"
fi

log "Compose up is $compose_up"

if [ "$compose_up" != "false" ]; then
    azureuser=$(grep -Eo '<UserName>.+</UserName>' /var/lib/waagent/ovf-env.xml | awk -F'[<>]' '{ print $3 }')
    log "Composing:"
    echo $compose_up | yaml_dump
    mkdir -p "/home/$azureuser/compose"
    pushd "/home/$azureuser/compose"
    echo $compose_up | yaml_dump > ./docker-compose.yml
    docker-compose up -d
    popd
fi

signal_success
