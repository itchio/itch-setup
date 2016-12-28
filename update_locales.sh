#!/bin/bash
LOCALES="$(jq -r '.locales[].value' < locales.json)"
LOCALES_BASE_URL="https://locales.itch.ovh/itch"

echo "Updating $(echo ${LOCALES} | awk '{print NF}') locales..."

parallel --no-notice curl -s ${LOCALES_BASE_URL}/{}.json -o data/locales/{}.json ::: ${LOCALES}

echo "Regenerating bindata"

go-bindata data/...

echo "Done."
