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

file_name=${0##*/}

timestamp() {
    date +"%Y-%m-%d %H:%M:%S"
}

log() {
    echo "$file_name $(timestamp): $1"
}

is_supported_distro() {
    [[ $1 == "CoreOS" || $1 == "Ubuntu" ]]
}

validate_distro() {
    distrib_id=$(awk -F'=' '{if($1=="DISTRIB_ID")print $2; }' /etc/*-release);

    if [ $distrib_id == "" ]; then
	log "Error reading DISTRIB_ID"
	exit 1
    fi

    if is_supported_distro $distrib_id; then 
	log "OS $distrib_id is supported."
    else
	log "OS $distrib_id is NOT supported."
	exit 1;
    fi
}

