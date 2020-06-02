//@ts-check
"use strict";

const { $, header, info } = require("@itchio/bob");

/**
 * @param {string[]} args
 */
function main(args) {
  let positional = [];
  for (const arg of args) {
    positional.push(arg);
  }

  header("Gathering configuration");
  $(`go version`);
  $(`go get -u github.com/go-bindata/go-bindata/...`);

  header("Updating locale data");

  info("Fetching locale data from repository");
  $(`rm -rf i18n-upstream`);
  $(
    `git clone --depth 1 https://github.com/itchio/itch-i18n.git i18n-upstream`
  );

  console.log(`Copying locales data`);
  $(`mkdir -p ./data/locales`);
  $(`cp -rfv i18n-upstream/locales/*.json ./data/locales/`);
  $(`rm -rf i18n-upstream`);

  header("Regenerating Go bindata package");
  $(`go-bindata -pkg bindata -o bindata/bindata.go data/...`);

  console.log(`Done.`);
}

main(process.argv.slice(2));
