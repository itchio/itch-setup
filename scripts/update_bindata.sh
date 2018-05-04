#!/bin/bash -xe

if [[ -z $1 ]]; then
echo "Usage: scripts/update_locales.sh PATH_TO_ITCH"
exit 1
fi

ITCH_PATH=$1

echo "Copying locales data from ${ITCH_PATH}"

cp -rfv ${ITCH_PATH}/src/static/locales* ./data/

echo "Regenerating bindata"

go-bindata -pkg bindata -o bindata/bindata.go data/...

echo "Done."
